// Package proquest reads ProQuest delivery ZIPs, parses their metadata through
// Crosswalk, and safely stages the package's primary and supplemental files.
package proquest

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	proquestfmt "github.com/lehigh-university-libraries/crosswalk/format/proquest"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const (
	defaultMaxEntries      = 1000
	defaultMaxArchiveBytes = int64(5 << 30)
	defaultMaxEntryBytes   = int64(2 << 30)
	defaultMaxTotalBytes   = int64(4 << 30)
	defaultMaxXMLBytes     = int64(16 << 20)
)

// BundleOptions controls archive limits and the explicit media destination.
type BundleOptions struct {
	MediaDirectory  string
	MaxEntries      int
	MaxArchiveBytes int64
	MaxEntryBytes   int64
	MaxTotalBytes   int64
	MaxXMLBytes     int64
}

// BundleResult contains parsed records and the media paths published for this
// package. MediaDirectory is package-specific and is renamed atomically only
// after every archive member has been validated and copied.
type BundleResult struct {
	Records        []*hubv1.Record
	MediaDirectory string
	Files          []string
}

// Metadata is the parsed record and content identity of a validated delivery.
// SHA256 binds a later media publication to the exact ZIP bytes reconciled by
// the caller.
type Metadata struct {
	Record *hubv1.Record
	SHA256 string
}

// BundleRequest identifies one previously inspected delivery for atomic batch
// publication.
type BundleRequest struct {
	ArchivePath    string
	ExpectedSHA256 string
}

// ReadMetadata validates a delivery ZIP and parses its single ProQuest record
// without creating or publishing any media paths. It is intended for duplicate
// reconciliation before an accepted package is staged with ReadBundle.
func ReadMetadata(ctx context.Context, archivePath string, options BundleOptions) (Metadata, error) {
	if ctx == nil {
		return Metadata{}, fmt.Errorf("reading ProQuest metadata: context is required")
	}
	archivePath = strings.TrimSpace(archivePath)
	if archivePath == "" {
		return Metadata{}, fmt.Errorf("reading ProQuest metadata: archive path is required")
	}
	snapshot, digest, err := snapshotArchive(ctx, archivePath, archiveLimit(options))
	if err != nil {
		return Metadata{}, err
	}
	reader, metadata, err := openSnapshotMetadata(ctx, snapshot, archivePath, options)
	if err != nil {
		return Metadata{}, errors.Join(err, removeFile(snapshot, "private ProQuest snapshot"))
	}
	if err := reader.Close(); err != nil {
		return Metadata{}, errors.Join(
			fmt.Errorf("closing private ProQuest snapshot: %w", err),
			removeFile(snapshot, "private ProQuest snapshot"),
		)
	}
	if err := removeFile(snapshot, "private ProQuest snapshot"); err != nil {
		return Metadata{}, err
	}
	return Metadata{Record: metadata.record, SHA256: digest}, nil
}

// ReadBundle parses one ProQuest ZIP and safely stages its media. Existing
// package destinations are rejected so repeated jobs cannot silently mix files
// from different deliveries.
func ReadBundle(ctx context.Context, archivePath string, options BundleOptions) (BundleResult, error) {
	metadata, err := ReadMetadata(ctx, archivePath, options)
	if err != nil {
		return BundleResult{}, err
	}
	return ReadBundles(ctx, []BundleRequest{{ArchivePath: archivePath, ExpectedSHA256: metadata.SHA256}}, options)
}

// ReadBundles validates, stages, and publishes an accepted delivery batch with
// one atomic directory rename. Every archive is copied to an immutable private
// snapshot and must match the digest produced by ReadMetadata.
func ReadBundles(ctx context.Context, requests []BundleRequest, options BundleOptions) (result BundleResult, err error) {
	if ctx == nil {
		return BundleResult{}, fmt.Errorf("reading ProQuest bundles: context is required")
	}
	if len(requests) == 0 {
		return BundleResult{}, fmt.Errorf("reading ProQuest bundles: at least one delivery is required")
	}
	mediaRoot, err := filepath.Abs(strings.TrimSpace(options.MediaDirectory))
	if err != nil || strings.TrimSpace(options.MediaDirectory) == "" {
		return BundleResult{}, fmt.Errorf("reading ProQuest bundles: media directory is required")
	}
	if !filepath.IsAbs(strings.TrimSpace(options.MediaDirectory)) {
		return BundleResult{}, fmt.Errorf("reading ProQuest bundles: media directory must be absolute")
	}
	if err := os.MkdirAll(mediaRoot, 0o750); err != nil {
		return BundleResult{}, fmt.Errorf("creating ProQuest media root: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp(mediaRoot, ".crosswalk-proquest-*")
	if err != nil {
		return BundleResult{}, fmt.Errorf("creating ProQuest batch workspace: %w", err)
	}
	type inspectedBundle struct {
		snapshot    string
		reader      *zip.ReadCloser
		metadata    parsedBundleMetadata
		packageName string
	}
	inspected := make([]inspectedBundle, 0, len(requests))
	keepTemporary := false
	defer func() {
		var cleanupErrors []error
		for index := range inspected {
			if inspected[index].reader == nil {
				continue
			}
			if closeErr := inspected[index].reader.Close(); closeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("closing private ProQuest snapshot: %w", closeErr))
			}
			inspected[index].reader = nil
		}
		if !keepTemporary {
			if removeErr := os.RemoveAll(temporaryDirectory); removeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("removing ProQuest batch workspace: %w", removeErr))
			}
		}
		if len(cleanupErrors) > 0 {
			err = errors.Join(append([]error{err}, cleanupErrors...)...)
		}
	}()

	packageNames := make(map[string]string, len(requests))
	batchHash := sha256.New()
	for index, request := range requests {
		archivePath := strings.TrimSpace(request.ArchivePath)
		expected := strings.ToLower(strings.TrimSpace(request.ExpectedSHA256))
		if archivePath == "" || !validSHA256(expected) {
			return BundleResult{}, fmt.Errorf("reading ProQuest bundles: delivery %d requires an archive path and SHA-256", index+1)
		}
		snapshot, digest, snapshotErr := snapshotArchiveInto(ctx, archivePath, temporaryDirectory, archiveLimit(options))
		if snapshotErr != nil {
			return BundleResult{}, snapshotErr
		}
		if digest != expected {
			return BundleResult{}, fmt.Errorf("reading ProQuest bundles: archive %q changed after metadata reconciliation", archivePath)
		}
		reader, metadata, metadataErr := openSnapshotMetadata(ctx, snapshot, archivePath, options)
		if metadataErr != nil {
			return BundleResult{}, metadataErr
		}
		packageName := safePackageName(archivePath) + "-" + digest[:16]
		if previous, exists := packageNames[packageName]; exists {
			if closeErr := reader.Close(); closeErr != nil {
				return BundleResult{}, errors.Join(
					fmt.Errorf("reading ProQuest bundles: %q and %q resolve to the same package destination", previous, archivePath),
					fmt.Errorf("closing private ProQuest snapshot: %w", closeErr),
				)
			}
			return BundleResult{}, fmt.Errorf("reading ProQuest bundles: %q and %q resolve to the same package destination", previous, archivePath)
		}
		packageNames[packageName] = archivePath
		if _, writeErr := io.WriteString(batchHash, packageName+"\x00"+digest+"\n"); writeErr != nil {
			if closeErr := reader.Close(); closeErr != nil {
				writeErr = errors.Join(writeErr, fmt.Errorf("closing private ProQuest snapshot: %w", closeErr))
			}
			return BundleResult{}, fmt.Errorf("hashing ProQuest batch identity: %w", writeErr)
		}
		inspected = append(inspected, inspectedBundle{snapshot: snapshot, reader: reader, metadata: metadata, packageName: packageName})
	}
	batchName := "proquest-" + hex.EncodeToString(batchHash.Sum(nil))[:16]
	finalDirectory := filepath.Join(mediaRoot, batchName)
	if _, err := os.Lstat(finalDirectory); err == nil {
		return BundleResult{}, fmt.Errorf("reading ProQuest bundles: media destination already exists: %s", finalDirectory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return BundleResult{}, fmt.Errorf("checking ProQuest media destination: %w", err)
	}

	records := make([]*hubv1.Record, 0, len(inspected))
	files := make([]string, 0)
	for _, bundle := range inspected {
		packageWorkspace := filepath.Join(temporaryDirectory, bundle.packageName)
		packageDestination := filepath.Join(finalDirectory, bundle.packageName)
		stagedByName := make(map[string]string, len(bundle.metadata.members))
		for _, member := range bundle.metadata.members {
			if member.name == bundle.metadata.xmlMember.name || ignoredPackageMember(member.name) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return BundleResult{}, err
			}
			target := filepath.Join(packageWorkspace, filepath.FromSlash(member.name))
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return BundleResult{}, fmt.Errorf("creating ProQuest media directory: %w", err)
			}
			if err := copyMember(ctx, member.file, target, entryLimit(options)); err != nil {
				return BundleResult{}, fmt.Errorf("extracting ProQuest media %q: %w", member.name, err)
			}
			finalPath := filepath.Join(packageDestination, filepath.FromSlash(member.name))
			stagedByName[member.name] = finalPath
			files = append(files, finalPath)
		}
		if err := attachFiles(bundle.metadata.record, bundle.metadata.members, bundle.metadata.primary, bundle.metadata.xmlMember.name, stagedByName); err != nil {
			return BundleResult{}, err
		}
		records = append(records, bundle.metadata.record)
	}
	for index := range inspected {
		if err := inspected[index].reader.Close(); err != nil {
			return BundleResult{}, fmt.Errorf("closing private ProQuest snapshot: %w", err)
		}
		inspected[index].reader = nil
		if err := removeFile(inspected[index].snapshot, "private ProQuest snapshot"); err != nil {
			return BundleResult{}, err
		}
		inspected[index].snapshot = ""
	}
	if err := os.Rename(temporaryDirectory, finalDirectory); err != nil {
		return BundleResult{}, fmt.Errorf("publishing ProQuest media batch: %w", err)
	}
	keepTemporary = true
	sort.Strings(files)
	return BundleResult{Records: records, MediaDirectory: finalDirectory, Files: files}, nil
}

// DiscardBatch removes a batch returned by ReadBundles when a later metadata
// publication step fails. It only accepts a direct, content-addressed child of
// the supplied media root.
func DiscardBatch(mediaRoot string, result BundleResult) error {
	root, err := filepath.Abs(strings.TrimSpace(mediaRoot))
	if err != nil || strings.TrimSpace(mediaRoot) == "" {
		return fmt.Errorf("discarding ProQuest batch: media root is required")
	}
	destination, err := filepath.Abs(strings.TrimSpace(result.MediaDirectory))
	if err != nil || strings.TrimSpace(result.MediaDirectory) == "" {
		return fmt.Errorf("discarding ProQuest batch: batch directory is required")
	}
	name := filepath.Base(destination)
	digest := strings.TrimPrefix(name, "proquest-")
	if filepath.Dir(destination) != root || len(digest) != 16 || !isLowerHex(digest) {
		return fmt.Errorf("discarding ProQuest batch: %q is not a content-addressed child of %q", destination, root)
	}
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("discarding ProQuest batch %q: %w", destination, err)
	}
	return nil
}

func openSnapshotMetadata(ctx context.Context, snapshotPath, sourceName string, options BundleOptions) (*zip.ReadCloser, parsedBundleMetadata, error) {
	reader, err := zip.OpenReader(snapshotPath)
	if err != nil {
		return nil, parsedBundleMetadata{}, fmt.Errorf("opening ProQuest bundle: %w", err)
	}
	metadata, err := parseBundleMetadata(ctx, sourceName, reader.File, options)
	if err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			return nil, parsedBundleMetadata{}, errors.Join(err, fmt.Errorf("closing private ProQuest snapshot: %w", closeErr))
		}
		return nil, parsedBundleMetadata{}, err
	}
	return reader, metadata, nil
}

func snapshotArchive(ctx context.Context, archivePath string, limit int64) (string, string, error) {
	return snapshotArchiveInto(ctx, archivePath, "", limit)
}

func snapshotArchiveInto(ctx context.Context, archivePath, directory string, limit int64) (snapshotPath, digest string, err error) {
	if limit <= 0 {
		return "", "", fmt.Errorf("snapshotting ProQuest bundle: archive byte limit must be positive")
	}
	source, err := os.Open(archivePath) // #nosec G304 -- archivePath is explicit caller input and is copied into an immutable private snapshot.
	if err != nil {
		return "", "", fmt.Errorf("opening ProQuest bundle: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(directory, ".crosswalk-proquest-archive-*.zip")
	if err != nil {
		return "", "", fmt.Errorf("creating private ProQuest snapshot: %w", err)
	}
	snapshotPath = temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(snapshotPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("securing private ProQuest snapshot: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(&contextReader{ctx: ctx, reader: source}, limit+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", "", fmt.Errorf("snapshotting ProQuest bundle: %w", copyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("closing private ProQuest snapshot: %w", closeErr)
	}
	if written > limit {
		return "", "", fmt.Errorf("reading ProQuest bundle: archive exceeds %d compressed bytes", limit)
	}
	keep = true
	return snapshotPath, hex.EncodeToString(hash.Sum(nil)), nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if character >= '0' && character <= '9' {
			continue
		}
		if character < 'a' || character > 'f' {
			return false
		}
	}
	return value != ""
}

type archiveMember struct {
	name string
	file *zip.File
}

type parsedBundleMetadata struct {
	record    *hubv1.Record
	members   []archiveMember
	xmlMember archiveMember
	primary   string
}

func parseBundleMetadata(ctx context.Context, archivePath string, files []*zip.File, options BundleOptions) (parsedBundleMetadata, error) {
	members, xmlMember, err := inspectArchive(ctx, files, options)
	if err != nil {
		return parsedBundleMetadata{}, err
	}
	xmlData, err := readMember(ctx, xmlMember.file, xmlLimit(options))
	if err != nil {
		return parsedBundleMetadata{}, fmt.Errorf("reading ProQuest metadata %q: %w", xmlMember.name, err)
	}
	records, err := (&proquestfmt.Format{}).Parse(bytes.NewReader(xmlData), &format.ParseOptions{SourceName: archivePath})
	if err != nil {
		return parsedBundleMetadata{}, fmt.Errorf("parsing ProQuest metadata: %w", err)
	}
	if len(records) != 1 {
		return parsedBundleMetadata{}, fmt.Errorf("reading ProQuest bundle: metadata contains %d submissions, want exactly one", len(records))
	}
	primary, err := selectPrimary(records[0], members, xmlMember.name)
	if err != nil {
		return parsedBundleMetadata{}, err
	}
	hub.SetExtra(records[0], "source_bundle", archivePath)
	return parsedBundleMetadata{record: records[0], members: members, xmlMember: xmlMember, primary: primary}, nil
}

func inspectArchive(ctx context.Context, files []*zip.File, options BundleOptions) ([]archiveMember, archiveMember, error) {
	maxEntries := options.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	if len(files) > maxEntries {
		return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: archive exceeds %d entries", maxEntries)
	}
	members := make([]archiveMember, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	var xmlMembers []archiveMember
	var total uint64
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, archiveMember{}, err
		}
		name, err := safeMemberName(file.Name)
		if err != nil {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: %w", err)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: member %q is not a regular file", file.Name)
		}
		if _, exists := seen[name]; exists {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: duplicate member %q", name)
		}
		seen[name] = struct{}{}
		if exceedsLimit(file.UncompressedSize64, entryLimit(options)) {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: member %q exceeds %d bytes", name, entryLimit(options))
		}
		if file.UncompressedSize64 > math.MaxUint64-total {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: expanded size overflows")
		}
		total += file.UncompressedSize64
		if exceedsLimit(total, totalLimit(options)) {
			return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: archive exceeds %d expanded bytes", totalLimit(options))
		}
		member := archiveMember{name: name, file: file}
		members = append(members, member)
		if strings.HasSuffix(strings.ToUpper(name), "_DATA.XML") {
			xmlMembers = append(xmlMembers, member)
		}
	}
	if len(xmlMembers) != 1 {
		return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: found %d _DATA.xml members, want exactly one", len(xmlMembers))
	}
	if exceedsLimit(xmlMembers[0].file.UncompressedSize64, xmlLimit(options)) {
		return nil, archiveMember{}, fmt.Errorf("reading ProQuest bundle: metadata member exceeds %d bytes", xmlLimit(options))
	}
	sort.Slice(members, func(i, j int) bool { return members[i].name < members[j].name })
	return members, xmlMembers[0], nil
}

func safeMemberName(name string) (string, error) {
	if strings.ContainsAny(name, "\\\x00\r\n") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive member path %q", name)
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != strings.TrimSuffix(name, "/") {
		return "", fmt.Errorf("unsafe archive member path %q", name)
	}
	return cleaned, nil
}

func readMember(ctx context.Context, file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("member exceeds %d bytes", limit)
	}
	return data, nil
}

func copyMember(ctx context.Context, file *zip.File, target string, limit int64) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limit+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		return fmt.Errorf("member exceeds %d bytes", limit)
	}
	return nil
}

func selectPrimary(record *hubv1.Record, members []archiveMember, xmlName string) (string, error) {
	declared := ""
	for _, file := range record.Files {
		if file != nil && file.Role == "primary" {
			declared = strings.TrimSpace(strings.ReplaceAll(file.Path, "\\", "/"))
			break
		}
	}
	var exact, basename []string
	for _, member := range members {
		if member.name == xmlName || ignoredPackageMember(member.name) {
			continue
		}
		if declared != "" && member.name == declared {
			exact = append(exact, member.name)
		}
		if declared != "" && path.Base(member.name) == path.Base(declared) {
			basename = append(basename, member.name)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(basename) == 1 {
		return basename[0], nil
	}
	if len(basename) > 1 {
		return "", fmt.Errorf("reading ProQuest bundle: primary filename %q is ambiguous", declared)
	}
	var pdfs []string
	for _, member := range members {
		if member.name != xmlName && strings.EqualFold(path.Ext(member.name), ".pdf") {
			pdfs = append(pdfs, member.name)
		}
	}
	if len(pdfs) == 1 {
		return pdfs[0], nil
	}
	if declared != "" {
		return "", fmt.Errorf("reading ProQuest bundle: declared primary file %q was not found", declared)
	}
	return "", fmt.Errorf("reading ProQuest bundle: found %d PDF candidates, want exactly one", len(pdfs))
}

func attachFiles(record *hubv1.Record, members []archiveMember, primary, xmlName string, staged map[string]string) error {
	record.Files = nil
	for _, member := range members {
		filePath, exists := staged[member.name]
		if !exists || member.name == xmlName || ignoredPackageMember(member.name) {
			continue
		}
		role := "supplemental"
		if member.name == primary {
			role = "primary"
		}
		size, err := archiveMemberSize(member.file.UncompressedSize64)
		if err != nil {
			return fmt.Errorf("attaching ProQuest media %q: %w", member.name, err)
		}
		record.Files = append(record.Files, &hubv1.File{
			Path:      filePath,
			Name:      path.Base(member.name),
			MimeType:  mime.TypeByExtension(strings.ToLower(path.Ext(member.name))),
			SizeBytes: size,
			Role:      role,
		})
	}
	return nil
}

func ignoredPackageMember(name string) bool {
	base := path.Base(name)
	return strings.HasPrefix(name, "__MACOSX/") || base == ".DS_Store"
}

func safePackageName(archivePath string) string {
	name := filepath.Base(archivePath)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.Trim(strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z':
			return character
		case character >= 'A' && character <= 'Z':
			return character
		case character >= '0' && character <= '9':
			return character
		case character == '-', character == '_', character == '.':
			return character
		default:
			return '-'
		}
	}, name), ".-_")
	if name == "" || name == "." {
		return "proquest-bundle"
	}
	return name
}

func archiveLimit(options BundleOptions) int64 {
	if options.MaxArchiveBytes > 0 {
		return options.MaxArchiveBytes
	}
	return defaultMaxArchiveBytes
}

func entryLimit(options BundleOptions) int64 {
	if options.MaxEntryBytes > 0 {
		return options.MaxEntryBytes
	}
	return defaultMaxEntryBytes
}

func totalLimit(options BundleOptions) int64 {
	if options.MaxTotalBytes > 0 {
		return options.MaxTotalBytes
	}
	return defaultMaxTotalBytes
}

func xmlLimit(options BundleOptions) int64 {
	if options.MaxXMLBytes > 0 {
		return options.MaxXMLBytes
	}
	return defaultMaxXMLBytes
}

func exceedsLimit(value uint64, limit int64) bool {
	return limit <= 0 || value > uint64(limit)
}

func archiveMemberSize(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("member size exceeds signed 64-bit range")
	}
	return int64(value), nil // #nosec G115 -- the range check above proves the conversion is safe.
}

func removeFile(name, description string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", description, err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
