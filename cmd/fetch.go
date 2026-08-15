package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	crossrefrestfmt "github.com/lehigh-university-libraries/crosswalk/format/crossrefrest"
	cslfmt "github.com/lehigh-university-libraries/crosswalk/format/csl"
	workbenchfmt "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	scopusfmt "github.com/lehigh-university-libraries/crosswalk/format/scopus"
	wosfmt "github.com/lehigh-university-libraries/crosswalk/format/wos"
	zenodofmt "github.com/lehigh-university-libraries/crosswalk/format/zenodo"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	"github.com/lehigh-university-libraries/crosswalk/source"
	arxivsource "github.com/lehigh-university-libraries/crosswalk/source/arxiv"
	crossrefsource "github.com/lehigh-university-libraries/crosswalk/source/crossref"
	directorysource "github.com/lehigh-university-libraries/crosswalk/source/directory"
	doisource "github.com/lehigh-university-libraries/crosswalk/source/doi"
	drupalsource "github.com/lehigh-university-libraries/crosswalk/source/drupal"
	mediasource "github.com/lehigh-university-libraries/crosswalk/source/media"
	proquestsource "github.com/lehigh-university-libraries/crosswalk/source/proquest"
	scopussource "github.com/lehigh-university-libraries/crosswalk/source/scopus"
	sherpasource "github.com/lehigh-university-libraries/crosswalk/source/sherpa"
	wossource "github.com/lehigh-university-libraries/crosswalk/source/wos"
	zenodosource "github.com/lehigh-university-libraries/crosswalk/source/zenodo"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/spf13/cobra"
)

type fetchOutputOptions struct {
	format            string
	path              string
	pretty            bool
	separator         string
	artifactDirectory string
	specPath          string
	transformation    *spec.Transformation
}

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Acquire scholarly metadata before converting it through Crosswalk",
		Long: `Acquire metadata from a public scholarly source and pass it through the same
Hub normalization and serialization pipeline used by crosswalk convert. Network
acquisition is kept separate from deterministic format parsing.`,
	}
	cmd.AddCommand(newFetchDOICmd())
	cmd.AddCommand(newFetchArxivCmd())
	cmd.AddCommand(newFetchCrossrefCmd())
	cmd.AddCommand(newFetchWOSCmd())
	cmd.AddCommand(newFetchScopusCmd())
	cmd.AddCommand(newFetchZenodoCmd())
	cmd.AddCommand(newFetchProQuestCmd())
	return cmd
}

func newFetchDOICmd() *cobra.Command {
	var inputPath string
	var endpoint string
	var maxRecords int
	var downloadFiles bool
	var mediaDirectory string
	var maxFileBytes int64
	var allowMissingFiles bool
	var sherpaEnrichment bool
	var sherpaAPIKeyEnvironment string
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "doi [DOI...]",
		Short: "Resolve DOI metadata as CSL-JSON and convert it",
		Long: `Resolve each DOI through HTTP content negotiation, parse the returned CSL-JSON
into Hub records, and serialize those records to the selected target format. DOI
arguments and --input may be combined; duplicates are acquired only once.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			identifiers, err := doiInputs(args, inputPath, maxRecords)
			if err != nil {
				return err
			}
			resolvedMediaDirectory := ""
			if downloadFiles {
				var resolveErr error
				resolvedMediaDirectory, resolveErr = resolveFetchMediaDirectory(mediaDirectory, output)
				if resolveErr != nil {
					return resolveErr
				}
			}
			apiKey := ""
			if sherpaEnrichment {
				apiKey = strings.TrimSpace(os.Getenv(sherpaAPIKeyEnvironment))
				if apiKey == "" {
					return fmt.Errorf("SHERPA enrichment requires a key in %s", sherpaAPIKeyEnvironment)
				}
			}
			client := doisource.NewClient()
			client.BaseURL = endpoint
			records := make([]*hubv1.Record, 0, len(identifiers))
			type doiAcquisition struct {
				identifier string
				cslJSON    []byte
			}
			acquired := make(map[*hubv1.Record]doiAcquisition, len(identifiers))
			parser := &cslfmt.Format{}
			for _, identifier := range identifiers {
				document, err := client.ResolveCSL(cmd.Context(), identifier)
				if err != nil {
					return err
				}
				parsed, err := parser.Parse(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
				if err != nil {
					return fmt.Errorf("parsing DOI %q metadata: %w", identifier, err)
				}
				for _, record := range parsed {
					records = append(records, record)
					acquired[record] = doiAcquisition{identifier: identifier, cslJSON: append([]byte(nil), document.Data...)}
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			downloader := &mediasource.Downloader{MaxBytes: maxFileBytes}
			sherpaClient := &sherpasource.Client{}
			for _, record := range reconciled {
				metadata, ok := acquired[record]
				if !ok {
					return fmt.Errorf("internal error: reconciled DOI record has no acquisition metadata")
				}
				if sherpaEnrichment {
					if err := enrichDOIWithSHERPA(cmd, sherpaClient, record, apiKey); err != nil {
						return fmt.Errorf("enriching DOI %q with SHERPA: %w", metadata.identifier, err)
					}
				}
				if downloadFiles {
					pdfURL, discoveryErr := client.PDFURL(cmd.Context(), metadata.cslJSON)
					if discoveryErr != nil {
						if !allowMissingFiles {
							return fmt.Errorf("discovering DOI %q PDF: %w", metadata.identifier, discoveryErr)
						}
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: DOI %s has no discoverable PDF: %v\n", metadata.identifier, discoveryErr)
						continue
					}
					result, err := downloader.Download(cmd.Context(), mediasource.Request{
						URL: pdfURL, Directory: resolvedMediaDirectory, Filename: mediasource.PDFName(metadata.identifier), ExpectedMediaType: "application/pdf",
					})
					if err != nil {
						return fmt.Errorf("downloading DOI %q PDF: %w", metadata.identifier, err)
					}
					attachDownloadedPDF(record, result)
				}
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "Read one DOI per line from this file in addition to DOI arguments")
	cmd.Flags().StringVar(&endpoint, "endpoint", doisource.DefaultBaseURL, "DOI content-negotiation base URL")
	cmd.Flags().IntVar(&maxRecords, "max-records", 1000, "Maximum unique DOI records to acquire")
	cmd.Flags().BoolVar(&downloadFiles, "download-files", false, "Discover and download available publisher PDFs")
	cmd.Flags().StringVar(&mediaDirectory, "media-dir", "", "Workbench staging subdirectory or allowed absolute /home or /mnt path for downloaded PDFs")
	cmd.Flags().Int64Var(&maxFileBytes, "max-file-bytes", 1<<30, "Maximum bytes allowed for one downloaded PDF")
	cmd.Flags().BoolVar(&allowMissingFiles, "allow-missing-files", false, "Continue when metadata has no discoverable PDF")
	cmd.Flags().BoolVar(&sherpaEnrichment, "sherpa", false, "Look up repository policy evidence by ISSN")
	cmd.Flags().StringVar(&sherpaAPIKeyEnvironment, "sherpa-api-key-env", "SHERPA_ROMEO_API_KEY", "Environment variable containing the SHERPA API key")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchArxivCmd() *cobra.Command {
	var queries []string
	var identifiers []string
	var start int
	var pageSize int
	var maxPages int
	var maxRecords int
	var endpoint string
	var oaiEndpoint string
	var oaiEnrichment bool
	var downloadFiles bool
	var mediaDirectory string
	var maxFileBytes int64
	var allowMissingFiles bool
	var directoryURL string
	var emails []string
	var emailsFile string
	var maxEmails int
	var requestDelay time.Duration
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "arxiv",
		Short: "Search arXiv, optionally enrich and download results, then convert them",
		Long: `Search explicitly bounded arXiv Atom pages by query, identifier, email, or a
bounded directory listing. Results may be enriched through arXiv OAI and their PDFs
downloaded before serialization. Pagination and delays remain explicit and bounded.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			if len(identifiers) > 0 && (len(queries) > 0 || len(emails) > 0 || strings.TrimSpace(emailsFile) != "" || strings.TrimSpace(directoryURL) != "") {
				return fmt.Errorf("--id cannot be combined with query, email, or directory selectors")
			}
			if len(identifiers) > 0 && start != 0 {
				return fmt.Errorf("--start cannot be used with explicit --id selectors")
			}
			if start < 0 || pageSize <= 0 || pageSize > 2000 || maxPages <= 0 || maxRecords <= 0 || requestDelay < 0 {
				return fmt.Errorf("--start and --request-delay must be non-negative; --page-size, --max-pages, and --max-records must be positive; --page-size must not exceed 2000")
			}
			resolvedMediaDirectory := ""
			if downloadFiles {
				var resolveErr error
				resolvedMediaDirectory, resolveErr = resolveFetchMediaDirectory(mediaDirectory, output)
				if resolveErr != nil {
					return resolveErr
				}
			}
			selectors, err := arxivSelectors(cmd, queries, identifiers, emails, emailsFile, directoryURL, maxEmails)
			if err != nil {
				return err
			}
			if len(identifiers) > 0 {
				identifierCount := len(selectors[0].identifiers)
				if identifierCount > maxRecords {
					return fmt.Errorf("%d unique arXiv IDs exceed --max-records %d; increase the explicit record limit", identifierCount, maxRecords)
				}
				selectors = chunkArxivIdentifierSelectors(selectors[0].identifiers, pageSize)
			}
			client := arxivsource.NewClient()
			client.APIBaseURL = endpoint
			client.OAIBaseURL = oaiEndpoint
			parser, err := format.GetParser("arxiv")
			if err != nil {
				return fmt.Errorf("loading arXiv parser: %w", err)
			}
			limiter := newRequestLimiter(requestDelay)
			records := make([]*hubv1.Record, 0)
			seen := make(map[string]struct{})
			for _, selector := range selectors {
				for page := 0; page < maxPages && len(records) < maxRecords; page++ {
					if err := limiter.Wait(cmd.Context()); err != nil {
						return err
					}
					document, err := client.Search(cmd.Context(), arxivsource.SearchOptions{
						Query:      selector.query,
						IDs:        selector.identifiers,
						Start:      start + page*pageSize,
						MaxResults: min(pageSize, maxRecords-len(records)),
					})
					if err != nil {
						return err
					}
					pageRecords, err := parser.Parse(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
					if err != nil {
						return fmt.Errorf("parsing arXiv metadata: %w", err)
					}
					for _, atomRecord := range pageRecords {
						key := arxivRecordKey(atomRecord)
						if key != "" {
							if _, exists := seen[key]; exists {
								continue
							}
							seen[key] = struct{}{}
						}
						record := atomRecord
						if selector.query != "" {
							hub.SetExtra(record, "arxiv_search_query", selector.query)
						}
						if oaiEnrichment {
							identifier := arxivRecordID(record)
							if identifier == "" {
								return fmt.Errorf("arXiv result %q has no arXiv identifier for OAI enrichment", record.Title)
							}
							if err := limiter.Wait(cmd.Context()); err != nil {
								return err
							}
							oaiDocument, err := client.OAI(cmd.Context(), identifier)
							if err != nil {
								return err
							}
							oaiRecords, err := parser.Parse(bytes.NewReader(oaiDocument.Data), &format.ParseOptions{SourceName: oaiDocument.URL})
							if err != nil {
								return fmt.Errorf("parsing arXiv OAI metadata for %q: %w", identifier, err)
							}
							if len(oaiRecords) != 1 {
								return fmt.Errorf("parsing arXiv OAI metadata for %q: got %d records, want 1", identifier, len(oaiRecords))
							}
							record = mergeArxivRecords(atomRecord, oaiRecords[0])
						}
						records = append(records, record)
						if len(records) == maxRecords {
							break
						}
					}
					if len(pageRecords) < pageSize || len(selector.identifiers) > 0 {
						break
					}
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			if downloadFiles {
				downloader := &mediasource.Downloader{MaxBytes: maxFileBytes}
				for _, record := range reconciled {
					pdfURL := hub.GetExtraString(record, "pdf_url")
					if pdfURL == "" {
						if !allowMissingFiles {
							return fmt.Errorf("arXiv result %q has no PDF URL", record.Title)
						}
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: arXiv result %q has no PDF URL\n", record.Title)
						continue
					}
					if err := limiter.Wait(cmd.Context()); err != nil {
						return err
					}
					identifier := arxivRecordID(record)
					result, err := downloader.Download(cmd.Context(), mediasource.Request{
						URL: pdfURL, Directory: resolvedMediaDirectory, Filename: mediasource.PDFName("arxiv:" + identifier), ExpectedMediaType: "application/pdf",
					})
					if err != nil {
						return fmt.Errorf("downloading arXiv %q PDF: %w", identifier, err)
					}
					attachDownloadedPDF(record, result)
				}
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringSliceVarP(&queries, "query", "q", nil, "arXiv API search expression; repeat or comma-separate")
	cmd.Flags().StringSliceVar(&identifiers, "id", nil, "arXiv identifier to retrieve; repeat or comma-separate")
	cmd.Flags().IntVar(&start, "start", 0, "Zero-based result offset for this page")
	cmd.Flags().IntVar(&pageSize, "page-size", 100, "Maximum records to retrieve per request")
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Maximum result pages to retrieve for each query")
	cmd.Flags().IntVar(&maxRecords, "max-records", 1000, "Maximum unique records to emit across all queries")
	cmd.Flags().StringVar(&endpoint, "endpoint", arxivsource.DefaultAPIBaseURL, "arXiv Atom query endpoint")
	cmd.Flags().StringVar(&oaiEndpoint, "oai-endpoint", arxivsource.DefaultOAIBaseURL, "arXiv OAI-PMH endpoint")
	cmd.Flags().BoolVar(&oaiEnrichment, "oai-enrich", true, "Enrich every result from the arXiv OAI record")
	cmd.Flags().BoolVar(&downloadFiles, "download-files", false, "Download result PDFs into --media-dir")
	cmd.Flags().StringVar(&mediaDirectory, "media-dir", "", "Workbench staging subdirectory or allowed absolute /home or /mnt path for downloaded PDFs")
	cmd.Flags().Int64Var(&maxFileBytes, "max-file-bytes", 1<<30, "Maximum bytes allowed for one downloaded PDF")
	cmd.Flags().BoolVar(&allowMissingFiles, "allow-missing-files", false, "Continue when a result has no PDF URL")
	cmd.Flags().StringVar(&directoryURL, "directory-url", "", "Bounded public directory page whose email addresses become search queries")
	cmd.Flags().StringSliceVar(&emails, "email", nil, "Email address to use as an arXiv query; repeat or comma-separate")
	cmd.Flags().StringVar(&emailsFile, "emails-file", "", "Read email search queries one per line")
	cmd.Flags().IntVar(&maxEmails, "max-emails", 1000, "Maximum unique directory/email selectors")
	cmd.Flags().DurationVar(&requestDelay, "request-delay", 3*time.Second, "Minimum delay between arXiv requests")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchCrossrefCmd() *cobra.Command {
	var query string
	var queryTitle string
	var queryAuthor string
	var filters []string
	var rows int
	var offset int
	var maxPages int
	var maxRecords int
	var sortField string
	var order string
	var mailto string
	var endpoint string
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "crossref",
		Short: "Search Crossref works and convert the bounded results",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			if rows <= 0 || rows > 1000 || offset < 0 || maxPages <= 0 || maxRecords <= 0 {
				return fmt.Errorf("--rows must be 1-1000, --offset non-negative, and page/record limits positive")
			}
			client := crossrefsource.NewClient()
			client.BaseURL = endpoint
			parser := &crossrefrestfmt.Format{}
			records := make([]*hubv1.Record, 0, min(rows*maxPages, maxRecords))
			seen := make(map[string]struct{})
			for page := 0; page < maxPages && len(records) < maxRecords; page++ {
				pageRows := min(rows, maxRecords-len(records))
				document, err := client.Search(cmd.Context(), crossrefsource.SearchOptions{
					Query: query, QueryTitle: queryTitle, QueryAuthor: queryAuthor, Filter: filters,
					Rows: pageRows, Offset: offset + page*rows, Sort: sortField, Order: order, Mailto: mailto,
				})
				if err != nil {
					return err
				}
				pageRecords, err := parser.Parse(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
				if err != nil {
					return err
				}
				for _, record := range pageRecords {
					key := recordIdentity(record)
					if key != "" {
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
					}
					records = append(records, record)
				}
				if len(pageRecords) < pageRows {
					break
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Crossref bibliographic query")
	cmd.Flags().StringVar(&queryTitle, "title", "", "Crossref title query")
	cmd.Flags().StringVar(&queryAuthor, "author", "", "Crossref author query")
	cmd.Flags().StringSliceVar(&filters, "filter", nil, "Crossref filter expression; repeat or comma-separate")
	cmd.Flags().IntVar(&rows, "rows", 100, "Results per request")
	cmd.Flags().IntVar(&offset, "offset", 0, "Initial result offset")
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Maximum result pages to retrieve")
	cmd.Flags().IntVar(&maxRecords, "max-records", 1000, "Maximum unique records to emit")
	cmd.Flags().StringVar(&sortField, "sort", "", "Crossref result sort field")
	cmd.Flags().StringVar(&order, "order", "", "Crossref result order (asc or desc)")
	cmd.Flags().StringVar(&mailto, "mailto", "", "Contact email for Crossref polite-pool requests")
	cmd.Flags().StringVar(&endpoint, "endpoint", crossrefsource.DefaultBaseURL, "Crossref REST works endpoint")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchWOSCmd() *cobra.Command {
	var query string
	var database string
	var page int
	var pageSize int
	var maxPages int
	var maxRecords int
	var sortField string
	var apiKeyEnvironment string
	var endpoint string
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "wos",
		Short: "Search Web of Science Starter and convert the bounded results",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			if page <= 0 || pageSize <= 0 || pageSize > 50 || maxPages <= 0 || maxRecords <= 0 {
				return fmt.Errorf("--page, --page-size, --max-pages, and --max-records must be positive; --page-size must not exceed 50")
			}
			apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironment))
			if apiKey == "" {
				return fmt.Errorf("web of Science API key is required in %s", apiKeyEnvironment)
			}
			client := wossource.NewClient(apiKey)
			client.BaseURL = endpoint
			parser := &wosfmt.Format{}
			records := make([]*hubv1.Record, 0, min(pageSize*maxPages, maxRecords))
			seen := make(map[string]struct{})
			for pageIndex := 0; pageIndex < maxPages && len(records) < maxRecords; pageIndex++ {
				limit := min(pageSize, maxRecords-len(records))
				document, err := client.Search(cmd.Context(), wossource.SearchOptions{
					Query: query, Database: database, Page: page + pageIndex, Limit: limit, SortField: sortField,
				})
				if err != nil {
					return err
				}
				pageRecords, err := parser.Parse(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
				if err != nil {
					return err
				}
				for _, record := range pageRecords {
					key := recordIdentity(record)
					if key != "" {
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
					}
					records = append(records, record)
				}
				if len(pageRecords) < limit {
					break
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringVarP(&query, "query", "q", "", "Web of Science advanced search query")
	cmd.Flags().StringVar(&database, "database", "WOS", "Web of Science database abbreviation")
	cmd.Flags().IntVar(&page, "page", 1, "Initial one-based result page")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "Results per request (maximum 50)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Maximum result pages to retrieve")
	cmd.Flags().IntVar(&maxRecords, "max-records", 500, "Maximum unique records to emit")
	cmd.Flags().StringVar(&sortField, "sort", "", "Web of Science sortField expression")
	cmd.Flags().StringVar(&apiKeyEnvironment, "api-key-env", "WOS_API_KEY", "Environment variable containing the Web of Science API key")
	cmd.Flags().StringVar(&endpoint, "endpoint", wossource.DefaultBaseURL, "Web of Science Starter API root")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchScopusCmd() *cobra.Command {
	var query string
	var start int
	var pageSize int
	var maxPages int
	var maxRecords int
	var view string
	var sortField string
	var dateRange string
	var cursorPagination bool
	var apiKeyEnvironment string
	var institutionTokenEnvironment string
	var accessTokenEnvironment string
	var endpoint string
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "scopus",
		Short: "Search Scopus and convert the bounded results",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			view = strings.ToUpper(strings.TrimSpace(view))
			if view != scopussource.ViewStandard && view != scopussource.ViewComplete {
				return fmt.Errorf("--view must be STANDARD or COMPLETE")
			}
			maxPageSize := 200
			if view == scopussource.ViewComplete {
				maxPageSize = 25
			}
			if start < 0 || pageSize <= 0 || pageSize > maxPageSize || maxPages <= 0 || maxRecords <= 0 {
				return fmt.Errorf("--start must be non-negative; --page-size must be 1-%d for the %s view; --max-pages and --max-records must be positive", maxPageSize, view)
			}
			if cursorPagination && start != 0 {
				return fmt.Errorf("--start cannot be used with cursor pagination; use --cursor=false for offset pagination")
			}
			apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironment))
			if apiKey == "" {
				return fmt.Errorf("scopus API key is required in %s", apiKeyEnvironment)
			}
			client := scopussource.NewClient(apiKey)
			client.BaseURL = endpoint
			client.InstitutionToken = strings.TrimSpace(os.Getenv(institutionTokenEnvironment))
			client.AccessToken = strings.TrimSpace(os.Getenv(accessTokenEnvironment))
			parser := &scopusfmt.Format{}
			records := make([]*hubv1.Record, 0, min(pageSize*maxPages, maxRecords))
			seen := make(map[string]struct{})
			cursorValue := ""
			if cursorPagination {
				cursorValue = "*"
			}
			for pageIndex := 0; pageIndex < maxPages && len(records) < maxRecords; pageIndex++ {
				count := min(pageSize, maxRecords-len(records))
				search := scopussource.SearchOptions{Query: query, Count: count, View: view, Sort: sortField, Date: dateRange}
				if cursorPagination {
					search.Cursor = cursorValue
				} else {
					search.Start = start + pageIndex*pageSize
				}
				document, err := client.Search(cmd.Context(), search)
				if err != nil {
					return err
				}
				page, err := parser.ParsePage(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
				if err != nil {
					return err
				}
				for _, record := range page.Records {
					key := recordIdentity(record)
					if key != "" {
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
					}
					records = append(records, record)
				}
				if len(page.Records) < count {
					break
				}
				if cursorPagination {
					next := strings.TrimSpace(page.NextCursor)
					if next == "" {
						break
					}
					if next == cursorValue {
						return fmt.Errorf("scopus returned the same pagination cursor twice")
					}
					cursorValue = next
				} else if page.TotalResults > 0 && start+(pageIndex+1)*pageSize >= page.TotalResults {
					break
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringVarP(&query, "query", "q", "", "Scopus boolean search expression")
	cmd.Flags().IntVar(&start, "start", 0, "Initial zero-based result offset")
	cmd.Flags().IntVar(&pageSize, "page-size", 25, "Results per request (STANDARD maximum 200; COMPLETE maximum 25)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Maximum result pages to retrieve")
	cmd.Flags().IntVar(&maxRecords, "max-records", 1000, "Maximum unique records to emit")
	cmd.Flags().StringVar(&view, "view", scopussource.ViewComplete, "Scopus response view: STANDARD or COMPLETE")
	cmd.Flags().StringVar(&sortField, "sort", "", "Scopus sort expression")
	cmd.Flags().StringVar(&dateRange, "date", "", "Scopus year or year range")
	cmd.Flags().BoolVar(&cursorPagination, "cursor", true, "Use cursor pagination to support result sets beyond the offset limit")
	cmd.Flags().StringVar(&apiKeyEnvironment, "api-key-env", "SCOPUS_API_KEY", "Environment variable containing the Scopus API key")
	cmd.Flags().StringVar(&institutionTokenEnvironment, "institution-token-env", "SCOPUS_INSTITUTION_TOKEN", "Environment variable containing an optional Scopus institution token")
	cmd.Flags().StringVar(&accessTokenEnvironment, "access-token-env", "SCOPUS_ACCESS_TOKEN", "Environment variable containing an optional Scopus OAuth access token")
	cmd.Flags().StringVar(&endpoint, "endpoint", scopussource.DefaultBaseURL, "Scopus Search API endpoint")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchZenodoCmd() *cobra.Command {
	var queries []string
	var recordIDs []string
	var page int
	var pageSize int
	var maxPages int
	var maxRecords int
	var sortField string
	var allVersions bool
	var accessTokenEnvironment string
	var endpoint string
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "zenodo",
		Short: "Retrieve published Zenodo records and convert them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			queries = compactUnique(queries, 0)
			recordIDs = compactUnique(recordIDs, 0)
			if len(queries) == 0 && len(recordIDs) == 0 {
				return fmt.Errorf("at least one --query or --id is required")
			}
			accessToken := strings.TrimSpace(os.Getenv(accessTokenEnvironment))
			maxPageSize := 25
			credentialDescription := "without an access token"
			if accessToken != "" {
				maxPageSize = 100
				credentialDescription = "with an access token"
			}
			if page <= 0 || pageSize <= 0 || pageSize > maxPageSize || maxPages <= 0 || maxRecords <= 0 {
				return fmt.Errorf("--page, --page-size, --max-pages, and --max-records must be positive; --page-size must not exceed %d %s", maxPageSize, credentialDescription)
			}
			client := zenodosource.NewClient()
			client.BaseURL = endpoint
			client.AccessToken = accessToken
			parser := &zenodofmt.Format{}
			records := make([]*hubv1.Record, 0, min(len(recordIDs)+pageSize*maxPages*max(1, len(queries)), maxRecords))
			seen := make(map[string]struct{})
			appendDocument := func(document *source.Document) (int, error) {
				parsed, err := parser.Parse(bytes.NewReader(document.Data), &format.ParseOptions{SourceName: document.URL})
				if err != nil {
					return 0, err
				}
				for _, record := range parsed {
					key := recordIdentity(record)
					if key != "" {
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
					}
					if len(records) < maxRecords {
						records = append(records, record)
					}
				}
				return len(parsed), nil
			}
			for _, recordID := range recordIDs {
				if len(records) == maxRecords {
					break
				}
				document, err := client.Record(cmd.Context(), recordID)
				if err != nil {
					return err
				}
				if _, err := appendDocument(document); err != nil {
					return err
				}
			}
			for _, query := range queries {
				for pageIndex := 0; pageIndex < maxPages && len(records) < maxRecords; pageIndex++ {
					size := min(pageSize, maxRecords-len(records))
					document, err := client.Search(cmd.Context(), zenodosource.SearchOptions{
						Query: query, Page: page + pageIndex, Size: size, Sort: sortField, AllVersions: allVersions,
					})
					if err != nil {
						return err
					}
					count, err := appendDocument(document)
					if err != nil {
						return err
					}
					if count < size {
						break
					}
				}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			return writeFetchedRecords(cmd, reconciled, output, reconcileOptions)
		},
	}
	cmd.Flags().StringSliceVarP(&queries, "query", "q", nil, "Zenodo Elasticsearch query; repeat or comma-separate")
	cmd.Flags().StringSliceVar(&recordIDs, "id", nil, "Numeric Zenodo published record ID; repeat or comma-separate")
	cmd.Flags().IntVar(&page, "page", 1, "Initial one-based search page")
	cmd.Flags().IntVar(&pageSize, "page-size", 25, "Search results per request (maximum 25 anonymous, 100 authenticated)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Maximum result pages to retrieve per query")
	cmd.Flags().IntVar(&maxRecords, "max-records", 1000, "Maximum unique records to emit")
	cmd.Flags().StringVar(&sortField, "sort", "", "Zenodo result sort expression")
	cmd.Flags().BoolVar(&allVersions, "all-versions", false, "Return every record version instead of only latest versions")
	cmd.Flags().StringVar(&accessTokenEnvironment, "access-token-env", "ZENODO_ACCESS_TOKEN", "Environment variable containing an optional Zenodo access token")
	cmd.Flags().StringVar(&endpoint, "endpoint", zenodosource.DefaultBaseURL, "Zenodo published records API endpoint")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func newFetchProQuestCmd() *cobra.Command {
	var inputDirectory string
	var recursive bool
	var mediaDirectory string
	var maxEntries int
	var maxArchiveBytes int64
	var maxEntryBytes int64
	var maxTotalBytes int64
	var maxXMLBytes int64
	reconcileOptions := reconcileCommandOptions{}
	output := fetchOutputOptions{}
	cmd := &cobra.Command{
		Use:   "proquest [ZIP...]",
		Short: "Parse ProQuest delivery ZIPs and stage their declared media",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := prepareFetchCommandOptions(cmd, &reconcileOptions, &output); err != nil {
				return err
			}
			resolvedMediaDirectory, err := resolveFetchMediaDirectory(mediaDirectory, output)
			if err != nil {
				return err
			}
			archives, err := proquestArchives(args, inputDirectory, recursive)
			if err != nil {
				return err
			}
			bundleOptions := proquestsource.BundleOptions{
				MediaDirectory: resolvedMediaDirectory,
				MaxEntries:     maxEntries, MaxArchiveBytes: maxArchiveBytes,
				MaxEntryBytes: maxEntryBytes, MaxTotalBytes: maxTotalBytes, MaxXMLBytes: maxXMLBytes,
			}
			records := make([]*hubv1.Record, 0, len(archives))
			type proquestAcquisition struct {
				archive string
				digest  string
			}
			acquired := make(map[*hubv1.Record]proquestAcquisition, len(archives))
			for index, archive := range archives {
				metadata, err := proquestsource.ReadMetadata(cmd.Context(), archive, bundleOptions)
				if err != nil {
					return err
				}
				record := metadata.Record
				if hub.GetExtraString(record, "id") == "" {
					hub.SetExtra(record, "id", fmt.Sprintf("proquest-%06d", index+1))
				}
				records = append(records, record)
				acquired[record] = proquestAcquisition{archive: archive, digest: metadata.SHA256}
			}
			reconciled, err := reconcileFetchedRecords(cmd, records, reconcileOptions)
			if err != nil {
				return err
			}
			if len(reconciled) == 0 {
				return writeFetchedRecords(cmd, nil, output, reconcileOptions)
			}
			requests := make([]proquestsource.BundleRequest, 0, len(reconciled))
			operationalIDs := make([]string, 0, len(reconciled))
			for _, accepted := range reconciled {
				acquisition, ok := acquired[accepted]
				if !ok {
					return fmt.Errorf("internal error: reconciled ProQuest record has no source archive")
				}
				requests = append(requests, proquestsource.BundleRequest{ArchivePath: acquisition.archive, ExpectedSHA256: acquisition.digest})
				operationalIDs = append(operationalIDs, hub.GetExtraString(accepted, "id"))
			}
			result, err := proquestsource.ReadBundles(cmd.Context(), requests, bundleOptions)
			if err != nil {
				return err
			}
			for index, record := range result.Records {
				hub.SetExtra(record, "id", operationalIDs[index])
			}
			if err := writeFetchedRecords(cmd, result.Records, output, reconcileOptions); err != nil {
				return errors.Join(err, proquestsource.DiscardBatch(resolvedMediaDirectory, result))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&inputDirectory, "input-dir", "", "Discover ProQuest ZIPs in this directory")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Recursively discover ZIPs below --input-dir")
	cmd.Flags().StringVar(&mediaDirectory, "media-dir", "", "Workbench staging subdirectory or allowed absolute /home or /mnt root for package media")
	cmd.Flags().IntVar(&maxEntries, "max-entries", 1000, "Maximum archive members per delivery")
	cmd.Flags().Int64Var(&maxArchiveBytes, "max-archive-bytes", 5<<30, "Maximum compressed bytes per delivery ZIP")
	cmd.Flags().Int64Var(&maxEntryBytes, "max-entry-bytes", 2<<30, "Maximum expanded bytes per archive member")
	cmd.Flags().Int64Var(&maxTotalBytes, "max-total-bytes", 4<<30, "Maximum total expanded bytes per archive")
	cmd.Flags().Int64Var(&maxXMLBytes, "max-xml-bytes", 16<<20, "Maximum metadata XML bytes per archive")
	bindReconcileFlags(cmd, &reconcileOptions)
	bindFetchOutputFlags(cmd, &output)
	return cmd
}

func bindFetchOutputFlags(cmd *cobra.Command, output *fetchOutputOptions) {
	cmd.Flags().StringVarP(&output.format, "to", "t", "islandora-workbench", "Target metadata format")
	cmd.Flags().StringVarP(&output.path, "output", "o", "", "Write output to this file instead of stdout")
	cmd.Flags().BoolVar(&output.pretty, "pretty", false, "Pretty-print formats that support it")
	cmd.Flags().StringVar(&output.separator, "separator", "|", "Target multi-value separator")
	cmd.Flags().StringVar(&output.artifactDirectory, "artifact-dir", "", "Write named Workbench operation artifacts to this directory")
	cmd.Flags().StringVar(&output.specPath, "spec", "", "Sealed Workbench transformation specification (profile-bound outputs require the same model fingerprint)")
}

func resolveFetchMediaDirectory(value string, output fetchOutputOptions) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("--media-dir is required")
	}
	if strings.EqualFold(output.format, "islandora-workbench") {
		transformation := output.transformation
		if transformation == nil {
			transformation = spec.FabricatorWorkbench()
		}
		normalized, err := transformation.NormalizeFilePath(value)
		if err != nil {
			return "", fmt.Errorf("resolving --media-dir as a Workbench staging path: %w", err)
		}
		if normalized == "" {
			return "", fmt.Errorf("--media-dir is required")
		}
		// Download into the exact normalized path emitted in the Workbench
		// artifact. Otherwise a relative local path would be rewritten in the
		// CSV without the corresponding file ever being staged there.
		return filepath.FromSlash(normalized), nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolving --media-dir: %w", err)
	}
	return absolute, nil
}

type reconcileCommandOptions struct {
	mode              string
	drupalJSONAPI     string
	drupalProfile     string
	drupalTokenEnv    string
	drupalUsernameEnv string
	drupalPasswordEnv string
	reportPath        string
	reviewCSVPath     string
	runtime           *reconcileRuntime
}

type reconcileRuntime struct {
	mode          reconcile.Mode
	policy        reconcile.Policy
	systemProfile *drupalReconciliationProfile
}

func bindReconcileFlags(cmd *cobra.Command, options *reconcileCommandOptions) {
	cmd.Flags().StringVar(&options.mode, "existing", string(reconcile.ModeHold), "Existing-item policy: hold, skip, force-new, or assume-new (repository lookup defaults to hold)")
	cmd.Flags().StringVar(&options.drupalJSONAPI, "drupal-jsonapi", "", "Drupal JSON:API root for existing-item lookup")
	cmd.Flags().StringVar(&options.drupalProfile, "drupal-profile", "", "stored Drupal profile defining repository fields and existing-item policy")
	cmd.Flags().StringVar(&options.drupalTokenEnv, "drupal-token-env", "DRUPAL_JSONAPI_TOKEN", "Environment variable containing an optional Drupal bearer token")
	cmd.Flags().StringVar(&options.drupalUsernameEnv, "drupal-username-env", "DRUPAL_JSONAPI_USERNAME", "Environment variable containing an optional Drupal Basic username")
	cmd.Flags().StringVar(&options.drupalPasswordEnv, "drupal-password-env", "DRUPAL_JSONAPI_PASSWORD", "Environment variable containing the Drupal Basic password")
	cmd.Flags().StringVar(&options.reportPath, "match-report", "", "Write the deterministic existing-item JSON report to this new file")
	cmd.Flags().StringVar(&options.reviewCSVPath, "match-review", "", "Write the formula-safe existing-item review CSV to this new file")
}

func reconcileFetchedRecords(cmd *cobra.Command, records []*hubv1.Record, options reconcileCommandOptions) ([]*hubv1.Record, error) {
	runtime := options.runtime
	if runtime == nil {
		var err error
		runtime, err = resolveReconcileCommandOptions(options)
		if err != nil {
			return nil, err
		}
	}
	mode := runtime.mode
	var finder reconcile.Finder
	if mode != reconcile.ModeAssumeNew {
		if runtime.systemProfile == nil {
			return nil, fmt.Errorf("a compiled Drupal profile is required for repository lookup")
		}
		client := drupalsource.NewClient(options.drupalJSONAPI)
		client.SystemProfile = runtime.systemProfile.compiled
		token := strings.TrimSpace(os.Getenv(options.drupalTokenEnv))
		username := strings.TrimSpace(os.Getenv(options.drupalUsernameEnv))
		password := os.Getenv(options.drupalPasswordEnv)
		switch {
		case token != "" && username != "":
			return nil, fmt.Errorf("configure either Drupal bearer token or Basic credentials, not both")
		case token != "":
			client.Auth = drupalsource.BearerTokenAuth(token)
		case username != "":
			client.Auth = drupalsource.BasicAuth{Username: username, Password: password}
		case password != "":
			return nil, fmt.Errorf("drupal password is configured without a username")
		}
		finder = client
	}
	inputs := make([]reconcile.Input, 0, len(records))
	for index, record := range records {
		key := hub.GetExtraString(record, "id")
		if key == "" {
			key = fmt.Sprintf("record-%d", index+1)
		}
		inputs = append(inputs, reconcile.Input{Key: key, Record: record})
	}
	provenance := reconcile.ReportProvenance{}
	if runtime.systemProfile != nil {
		provenance = runtime.systemProfile.provenance
	}
	report, err := (reconcile.Detector{Finder: finder, Policy: runtime.policy, Provenance: provenance}).Detect(cmd.Context(), inputs, mode)
	if err != nil {
		return nil, err
	}
	review, err := reconcile.ReviewCSV(report)
	if err != nil {
		return nil, err
	}
	if options.reportPath != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encoding match report: %w", err)
		}
		if err := writeNewFile(options.reportPath, append(encoded, '\n')); err != nil {
			return nil, fmt.Errorf("writing match report: %w", err)
		}
	}
	if options.reviewCSVPath != "" {
		if err := writeNewFile(options.reviewCSVPath, review); err != nil {
			return nil, fmt.Errorf("writing match review: %w", err)
		}
	}
	partition, err := reconcile.PartitionInputsWithPolicy(inputs, report, runtime.policy)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "existing-item review: total=%d new=%d duplicate=%d review=%d ambiguous=%d accepted=%d skipped=%d held=%d\n",
		report.Summary.Total, report.Summary.New, report.Summary.Duplicate, report.Summary.Review, report.Summary.Ambiguous,
		len(partition.Accepted), len(partition.Skipped), len(partition.Held))
	reviewLocation, err := emitExistingItemReview(cmd, review, options.reviewCSVPath, len(partition.Skipped) > 0 || partition.ReviewRequired)
	if err != nil {
		return nil, err
	}
	if len(partition.Skipped) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipped %d confirmed duplicate record(s); inspect %s to reconcile incoming metadata with the existing items\n", len(partition.Skipped), reviewLocation)
	}
	if partition.ReviewRequired {
		return nil, fmt.Errorf("existing-item review required for %d record(s); inspect %s before choosing update or force-new", len(partition.Held), reviewLocation)
	}
	accepted := make([]*hubv1.Record, 0, len(partition.Accepted))
	for _, input := range partition.Accepted {
		accepted = append(accepted, input.Record)
	}
	return accepted, nil
}

func emitExistingItemReview(cmd *cobra.Command, review []byte, configuredPath string, required bool) (string, error) {
	if configuredPath != "" {
		return configuredPath, nil
	}
	if !required {
		return "the generated review report", nil
	}
	if _, err := cmd.ErrOrStderr().Write(review); err != nil {
		return "", fmt.Errorf("writing existing-item review to stderr: %w", err)
	}
	return "the review CSV on stderr (or rerun with --match-review for a durable artifact)", nil
}

func validateReconcileCommandOptions(options reconcileCommandOptions) (reconcile.Mode, error) {
	runtime, err := resolveReconcileCommandOptions(options)
	if err != nil {
		return "", err
	}
	return runtime.mode, nil
}

func prepareReconcileCommandOptions(options *reconcileCommandOptions) (reconcile.Mode, error) {
	if options == nil {
		return "", fmt.Errorf("reconciliation options are required")
	}
	runtime, err := resolveReconcileCommandOptions(*options)
	if err != nil {
		return "", err
	}
	options.runtime = runtime
	return runtime.mode, nil
}

func prepareFetchCommandOptions(command *cobra.Command, reconcileOptions *reconcileCommandOptions, output *fetchOutputOptions) error {
	if output == nil {
		return fmt.Errorf("fetch output options are required")
	}
	if _, err := prepareReconcileCommandOptions(reconcileOptions); err != nil {
		return err
	}
	if err := resolveFetchTransformation(output, *reconcileOptions); err != nil {
		return err
	}
	if output.transformation != nil {
		if command != nil && command.Flags().Changed("separator") {
			return fmt.Errorf("--separator cannot be combined with a profile-bound or explicit --spec; edit and reseal the specification")
		}
		output.separator = output.transformation.Target.MultiValueSeparator
	}
	return nil
}

func resolveFetchTransformation(output *fetchOutputOptions, reconcileOptions reconcileCommandOptions) error {
	if output == nil {
		return fmt.Errorf("fetch output options are required")
	}
	if !strings.EqualFold(output.format, "islandora-workbench") {
		if strings.TrimSpace(output.specPath) != "" {
			return fmt.Errorf("--spec requires --to islandora-workbench")
		}
		output.transformation = nil
		return nil
	}
	transformation := output.transformation
	if strings.TrimSpace(output.specPath) != "" {
		var err error
		transformation, err = loadTransformationSpec(output.specPath, "csv", "islandora-workbench")
		if err != nil {
			return err
		}
	}
	var systemProfile *drupalReconciliationProfile
	if reconcileOptions.runtime != nil {
		systemProfile = reconcileOptions.runtime.systemProfile
	}
	if transformation == nil && systemProfile != nil {
		transformation = systemProfile.transformation
	}
	if transformation != nil && transformation.Fingerprint.Profile != "" && systemProfile == nil {
		return fmt.Errorf("profile-bound --spec requires the exact --drupal-profile")
	}
	if transformation != nil && transformation.Fingerprint.Profile == "" && systemProfile != nil {
		return fmt.Errorf("unbound --spec cannot be combined with --drupal-profile")
	}
	if transformation != nil && systemProfile != nil {
		if transformation.Fingerprint.Model != systemProfile.compiled.ModelFingerprint() {
			return fmt.Errorf("transformation model fingerprint does not match Drupal profile model")
		}
		if transformation.Fingerprint.Profile != systemProfile.compiled.Fingerprint() {
			return fmt.Errorf("transformation profile fingerprint does not match Drupal profile")
		}
	}
	output.transformation = transformation
	return nil
}

func resolveReconcileCommandOptions(options reconcileCommandOptions) (*reconcileRuntime, error) {
	mode := reconcile.Mode(strings.TrimSpace(options.mode))
	if mode == "" {
		mode = reconcile.ModeHold
	}
	if err := reconcile.ValidateMode(mode); err != nil {
		return nil, err
	}
	endpointConfigured := strings.TrimSpace(options.drupalJSONAPI) != ""
	profileConfigured := strings.TrimSpace(options.drupalProfile) != ""
	if endpointConfigured && !profileConfigured {
		return nil, fmt.Errorf("--drupal-profile is required with --drupal-jsonapi")
	}
	if mode != reconcile.ModeAssumeNew && !endpointConfigured {
		return nil, fmt.Errorf("--drupal-jsonapi is required for --existing %s; use --existing assume-new only when repository lookup is intentionally bypassed", mode)
	}
	runtime := &reconcileRuntime{mode: mode, policy: reconcile.PolicyV1()}
	if profileConfigured {
		systemProfile, err := loadDrupalReconciliationProfile(options.drupalProfile, mode != reconcile.ModeAssumeNew)
		if err != nil {
			return nil, err
		}
		runtime.systemProfile = systemProfile
		runtime.policy = systemProfile.policy
	}
	return runtime, nil
}

func doiInputs(args []string, inputPath string, maxRecords int) ([]string, error) {
	if maxRecords <= 0 {
		return nil, fmt.Errorf("--max-records must be positive")
	}
	seen := make(map[string]struct{}, min(len(args), maxRecords))
	identifiers := make([]string, 0, min(len(args), maxRecords))
	appendValue := func(raw string) error {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return nil
		}
		if len(identifiers) == maxRecords {
			return fmt.Errorf("unique DOI count exceeds --max-records %d", maxRecords)
		}
		seen[key] = struct{}{}
		identifiers = append(identifiers, value)
		return nil
	}
	for _, value := range args {
		if err := appendValue(value); err != nil {
			return nil, err
		}
	}
	if inputPath != "" {
		file, err := os.Open(inputPath)
		if err != nil {
			return nil, fmt.Errorf("opening DOI input: %w", err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if err := appendValue(scanner.Text()); err != nil {
				return nil, err
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("reading DOI input: %w", err)
		}
	}

	if len(identifiers) == 0 {
		return nil, fmt.Errorf("at least one DOI argument or --input value is required")
	}
	return identifiers, nil
}

func enrichDOIWithSHERPA(cmd *cobra.Command, client *sherpasource.Client, record *hubv1.Record, apiKey string) error {
	issn := ""
	for _, identifier := range record.Identifiers {
		if identifier != nil && identifier.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN {
			issn = identifier.Value
			break
		}
	}
	if issn == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %q has no ISSN for SHERPA lookup\n", record.Title)
		return nil
	}
	recommendation, err := client.Lookup(cmd.Context(), issn, apiKey)
	if err != nil {
		return err
	}
	hub.SetExtra(record, "sherpa_policy", map[string]any{
		"issn": recommendation.ISSN, "publication_id": recommendation.PublicationID,
		"policy_uri": recommendation.PolicyURI, "license_uri": recommendation.LicenseURI,
		"article_version": recommendation.ArticleVersion, "locations": recommendation.Locations,
		"conditions": recommendation.Conditions, "embargo_amount": recommendation.EmbargoAmount,
		"embargo_units": recommendation.EmbargoUnits, "review_required": recommendation.ReviewRequired,
	})
	if recommendation.ReviewRequired {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: SHERPA policy for ISSN %s requires manual review; evidence retained in metadata\n", issn)
		return nil
	}
	if recommendation.LicenseURI != "" {
		record.Rights = append(record.Rights, &hubv1.Rights{Uri: recommendation.LicenseURI})
	}
	return nil
}

func attachDownloadedPDF(record *hubv1.Record, result mediasource.Result) {
	record.Files = append(record.Files, &hubv1.File{
		Path: result.Path, Name: filepath.Base(result.Path), MimeType: "application/pdf", SizeBytes: result.SizeBytes, Role: "primary",
	})
	hub.SetExtra(record, "pdf_url", result.URL)
}

type arxivSelector struct {
	query       string
	identifiers []string
}

func arxivSelectors(cmd *cobra.Command, queries, identifiers, emails []string, emailsFile, directoryURL string, maxEmails int) ([]arxivSelector, error) {
	if maxEmails <= 0 {
		return nil, fmt.Errorf("--max-emails must be positive")
	}
	if len(identifiers) > 0 {
		values := compactUnique(identifiers, 0)
		if len(values) == 0 {
			return nil, fmt.Errorf("at least one non-empty --id is required")
		}
		if len(values) > 2000 {
			return nil, fmt.Errorf("more than 2000 unique arXiv IDs were supplied")
		}
		return []arxivSelector{{identifiers: values}}, nil
	}
	values := append([]string(nil), queries...)
	values = append(values, emails...)
	if strings.TrimSpace(emailsFile) != "" {
		fileValues, err := readLineValues(emailsFile, maxEmails)
		if err != nil {
			return nil, fmt.Errorf("reading arXiv email selectors: %w", err)
		}
		values = append(values, fileValues...)
	}
	if strings.TrimSpace(directoryURL) != "" {
		client := directorysource.NewClient()
		client.MaxEmails = maxEmails
		directoryEmails, err := client.Emails(cmd.Context(), directoryURL)
		if err != nil {
			return nil, err
		}
		values = append(values, directoryEmails...)
	}
	values = compactUnique(values, 0)
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one --query, --id, --email, --emails-file, or --directory-url selector is required")
	}
	if len(values) > maxEmails {
		return nil, fmt.Errorf("more than %d unique arXiv query/email selectors were supplied", maxEmails)
	}
	selectors := make([]arxivSelector, 0, len(values))
	for _, value := range values {
		selectors = append(selectors, arxivSelector{query: value})
	}
	return selectors, nil
}

func chunkArxivIdentifierSelectors(identifiers []string, size int) []arxivSelector {
	if size <= 0 {
		return nil
	}
	selectors := make([]arxivSelector, 0, (len(identifiers)+size-1)/size)
	for start := 0; start < len(identifiers); start += size {
		end := min(start+size, len(identifiers))
		selectors = append(selectors, arxivSelector{identifiers: append([]string(nil), identifiers[start:end]...)})
	}
	return selectors
}

func readLineValues(path string, maxValues int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	values := make([]string, 0)
	for scanner.Scan() {
		if value := strings.TrimSpace(scanner.Text()); value != "" {
			values = append(values, value)
			if len(values) > maxValues {
				return nil, fmt.Errorf("file contains more than %d non-empty values", maxValues)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func compactUnique(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		if limit > 0 && len(result) == limit {
			break
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

type requestLimiter struct {
	delay time.Duration
	last  time.Time
}

func newRequestLimiter(delay time.Duration) *requestLimiter { return &requestLimiter{delay: delay} }

func (limiter *requestLimiter) Wait(ctx context.Context) error {
	if limiter.delay <= 0 || limiter.last.IsZero() {
		limiter.last = time.Now()
		return nil
	}
	wait := time.Until(limiter.last.Add(limiter.delay))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	limiter.last = time.Now()
	return nil
}

func arxivRecordID(record *hubv1.Record) string {
	for _, identifier := range record.Identifiers {
		if identifier != nil && identifier.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV {
			return identifier.Value
		}
	}
	return ""
}

func arxivRecordKey(record *hubv1.Record) string {
	if identifier := arxivRecordID(record); identifier != "" {
		return "arxiv:" + strings.ToLower(identifier)
	}
	return recordIdentity(record)
}

func mergeArxivRecords(atomRecord, oaiRecord *hubv1.Record) *hubv1.Record {
	if oaiRecord.Title == "" {
		oaiRecord.Title = atomRecord.Title
	}
	if oaiRecord.Abstract == "" {
		oaiRecord.Abstract = atomRecord.Abstract
	}
	if oaiRecord.ResourceType == nil {
		oaiRecord.ResourceType = atomRecord.ResourceType
	}
	if len(oaiRecord.Contributors) == 0 {
		oaiRecord.Contributors = atomRecord.Contributors
	}
	if len(oaiRecord.Dates) == 0 {
		oaiRecord.Dates = atomRecord.Dates
	}
	if len(oaiRecord.Subjects) == 0 {
		oaiRecord.Subjects = atomRecord.Subjects
	}
	seenIdentifiers := make(map[string]struct{})
	for _, identifier := range oaiRecord.Identifiers {
		if identifier != nil {
			seenIdentifiers[fmt.Sprintf("%d:%s", identifier.Type, strings.ToLower(identifier.Value))] = struct{}{}
		}
	}
	for _, identifier := range atomRecord.Identifiers {
		if identifier == nil {
			continue
		}
		key := fmt.Sprintf("%d:%s", identifier.Type, strings.ToLower(identifier.Value))
		if _, exists := seenIdentifiers[key]; !exists {
			oaiRecord.Identifiers = append(oaiRecord.Identifiers, identifier)
			seenIdentifiers[key] = struct{}{}
		}
	}
	for _, key := range []string{"pdf_url", "arxiv_search_query"} {
		if value := hub.GetExtraString(atomRecord, key); value != "" {
			hub.SetExtra(oaiRecord, key, value)
		}
	}
	return oaiRecord
}

func recordIdentity(record *hubv1.Record) string {
	for _, identifier := range reconcile.Identifiers(record) {
		if reconcile.IsStrongIdentifier(identifier) {
			return strings.Join([]string{
				identifier.Scheme,
				identifier.NamespaceURI,
				identifier.IdentityLevel.String(),
				strings.ToLower(identifier.Value),
			}, "\x00")
		}
	}
	return ""
}

func proquestArchives(arguments []string, inputDirectory string, recursive bool) ([]string, error) {
	archives := append([]string(nil), arguments...)
	if strings.TrimSpace(inputDirectory) != "" {
		root, err := filepath.Abs(inputDirectory)
		if err != nil {
			return nil, fmt.Errorf("resolving ProQuest input directory: %w", err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("reading ProQuest input directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("ProQuest input path is not a directory: %s", root)
		}
		if recursive {
			err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
					archives = append(archives, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walking ProQuest input directory: %w", err)
			}
		} else {
			entries, err := os.ReadDir(root)
			if err != nil {
				return nil, fmt.Errorf("reading ProQuest input directory: %w", err)
			}
			for _, entry := range entries {
				if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
					archives = append(archives, filepath.Join(root, entry.Name()))
				}
			}
		}
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(archives))
	for _, archive := range archives {
		if strings.TrimSpace(archive) == "" {
			return nil, fmt.Errorf("ProQuest archive path is empty")
		}
		absolute, err := filepath.Abs(strings.TrimSpace(archive))
		if err != nil {
			return nil, fmt.Errorf("resolving ProQuest archive %q: %w", archive, err)
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, fmt.Errorf("reading ProQuest archive %q: %w", absolute, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(absolute), ".zip") {
			return nil, fmt.Errorf("ProQuest archive is not a regular ZIP: %s", absolute)
		}
		if _, exists := seen[absolute]; !exists {
			seen[absolute] = struct{}{}
			result = append(result, absolute)
		}
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one ProQuest ZIP argument or --input-dir ZIP is required")
	}
	return result, nil
}

func writeFetchedRecords(cmd *cobra.Command, records []*hubv1.Record, output fetchOutputOptions, reconcileOptions reconcileCommandOptions) (err error) {
	if err := resolveFetchTransformation(&output, reconcileOptions); err != nil {
		return err
	}
	if strings.EqualFold(output.format, "islandora-workbench") && output.transformation == nil {
		output.transformation, err = compatibilityWorkbenchSpec(output.separator)
		if err != nil {
			return err
		}
	}
	if output.transformation != nil {
		output.separator = output.transformation.Target.MultiValueSeparator
	}
	if output.artifactDirectory != "" {
		if !strings.EqualFold(output.format, "islandora-workbench") {
			return fmt.Errorf("--artifact-dir requires --to islandora-workbench")
		}
		if output.path != "" {
			return fmt.Errorf("--artifact-dir and --output cannot be combined")
		}
		prepareFetchedWorkbenchRecords(records)
		serializeOptions := &format.SerializeOptions{Spec: output.transformation, IncludeHeader: true, MultiValueSeparator: output.separator}
		if reconcileOptions.runtime != nil && reconcileOptions.runtime.systemProfile != nil {
			serializeOptions.SystemProfile = reconcileOptions.runtime.systemProfile.compiled
		}
		plan, err := workbenchfmt.PlanArtifacts(records, serializeOptions)
		if err != nil {
			return fmt.Errorf("planning Workbench artifacts: %w", err)
		}
		artifactDirectory, err := filepath.Abs(output.artifactDirectory)
		if err != nil {
			return fmt.Errorf("resolving artifact directory: %w", err)
		}
		outputs := make([]namedOutput, 0, len(plan.Artifacts))
		for _, artifact := range plan.Artifacts {
			if artifact.Name == "" || filepath.Base(artifact.Name) != artifact.Name {
				return fmt.Errorf("invalid Workbench artifact name %q", artifact.Name)
			}
			outputs = append(outputs, namedOutput{name: artifact.Name, data: artifact.Data})
		}
		if err := writeOutputDirectory(artifactDirectory, outputs); err != nil {
			return fmt.Errorf("writing Workbench artifacts: %w", err)
		}
		for _, artifact := range plan.Artifacts {
			path := filepath.Join(artifactDirectory, artifact.Name)
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d records)\n", path, artifact.Records)
		}
		return nil
	}
	serializer, err := format.GetSerializer(output.format)
	if err != nil {
		return fmt.Errorf("loading target format %q: %w", output.format, err)
	}
	if strings.EqualFold(output.format, "islandora-workbench") {
		prepareFetchedWorkbenchRecords(records)
	}

	serializeOptions := &format.SerializeOptions{
		IncludeHeader:       true,
		Pretty:              output.pretty,
		MultiValueSeparator: output.separator,
		Spec:                output.transformation,
	}
	if reconcileOptions.runtime != nil && reconcileOptions.runtime.systemProfile != nil {
		serializeOptions.SystemProfile = reconcileOptions.runtime.systemProfile.compiled
	}
	serialize := func(writer io.Writer) error {
		if err := serializer.Serialize(writer, records, serializeOptions); err != nil {
			return fmt.Errorf("serializing fetched metadata: %w", err)
		}
		return nil
	}
	if output.path != "" {
		return writeOutputFile(output.path, serialize)
	}
	return serialize(cmd.OutOrStdout())
}

func prepareFetchedWorkbenchRecords(records []*hubv1.Record) {
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.FullTitle == "" {
			record.FullTitle = record.Title
		}
		if title := []rune(record.Title); len(title) > 255 {
			record.Title = string(title[:255])
		}
		if record.ObjectModel == "" {
			record.ObjectModel = "Digital Document"
		}
	}
}
