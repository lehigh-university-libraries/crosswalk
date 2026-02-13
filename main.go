package main

import (
	"github.com/lehigh-university-libraries/crosswalk/cmd"

	// Register format plugins
	_ "github.com/lehigh-university-libraries/crosswalk/format/arxiv"
	_ "github.com/lehigh-university-libraries/crosswalk/format/bibtex"
	_ "github.com/lehigh-university-libraries/crosswalk/format/csv"
	_ "github.com/lehigh-university-libraries/crosswalk/format/drupal"
	_ "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	_ "github.com/lehigh-university-libraries/crosswalk/format/schemaorg"
)

func main() {
	cmd.Execute()
}
