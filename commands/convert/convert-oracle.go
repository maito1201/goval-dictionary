package convert

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"

	"github.com/inconshreveable/log15"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vulsio/goval-dictionary/fetcher"
	"github.com/vulsio/goval-dictionary/models"
	"github.com/vulsio/goval-dictionary/util"
	"github.com/ymomoi/goval-parser/oval"
	"golang.org/x/xerrors"
)

var convertOracleCmd = &cobra.Command{
	Use:   "oracle",
	Short: "Convert Vulnerability dictionary from Oracle",
	Long:  `Convert Vulnerability dictionary from Oracle`,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		if err := viper.BindPFlag("vuln-dir", cmd.Parent().PersistentFlags().Lookup("vuln-dir")); err != nil {
			return err
		}

		if err := viper.BindPFlag("http-proxy", cmd.Parent().PersistentFlags().Lookup("http-proxy")); err != nil {
			return err
		}

		return nil
	},
	RunE: convertOracle,
}

func convertOracle(_ *cobra.Command, _ []string) (err error) {
	if err := util.SetLogger(viper.GetBool("log-to-file"), viper.GetString("log-dir"), viper.GetBool("debug"), viper.GetBool("log-json")); err != nil {
		return xerrors.Errorf("Failed to SetLogger. err: %w", err)
	}

	vulnDir := filepath.Join(viper.GetString("vuln-dir"), "oracle")
	if f, err := os.Stat(vulnDir); err != nil {
		if !os.IsNotExist(err) {
			return xerrors.Errorf("Failed to check vuln directory. err: %w", err)
		}
		if err := os.MkdirAll(vulnDir, 0700); err != nil {
			return xerrors.Errorf("Failed to create vuln directory. err: %w", err)
		}
	} else if !f.IsDir() {
		return xerrors.Errorf("Failed to check vuln directory. err: %s is not directory", vulnDir)
	}

	log15.Info("Fetching Oracle CVEs")
	results, err := fetcher.FetchOracleFiles()
	if err != nil {
		return xerrors.Errorf("Failed to fetch files. err: %w", err)
	}

	log15.Info("Converting Oracle CVEs")
	verDefsMap := map[string]map[string][]models.Definition{}
	for _, r := range results {
		ovalroot := oval.Root{}
		if err = xml.Unmarshal(r.Body, &ovalroot); err != nil {
			return xerrors.Errorf("Failed to unmarshal xml. url: %s, err: %w", r.URL, err)
		}

		for osVer, defs := range models.ConvertOracleToModel(&ovalroot) {
			verDefsMap[osVer] = map[string][]models.Definition{}
			for _, def := range defs {
				for _, cve := range def.Advisory.Cves {
					verDefsMap[osVer][cve.CveID] = append(verDefsMap[osVer][cve.CveID], models.Definition{
						DefinitionID: def.DefinitionID,
						Title:        def.Title,
						Description:  def.Description,
						Advisory: models.Advisory{
							Severity:        def.Advisory.Severity,
							Cves:            []models.Cve{cve},
							Bugzillas:       def.Advisory.Bugzillas,
							AffectedCPEList: def.Advisory.AffectedCPEList,
							Issued:          def.Advisory.Issued,
							Updated:         def.Advisory.Updated,
						},
						Debian:        def.Debian,
						AffectedPacks: def.AffectedPacks,
						References:    def.References,
					})
				}
			}
		}
	}

	log15.Info("Deleting Old Oracle CVEs")
	dirs, err := filepath.Glob(filepath.Join(vulnDir, "*"))
	if err != nil {
		return xerrors.Errorf("Failed to get all dirs in vuln directory. err: %w", err)
	}
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return xerrors.Errorf("Failed to remove vuln data file. err: %w", err)
		}
	}

	log15.Info("Creating Oracle CVEs")
	for ver, defs := range verDefsMap {
		if err := os.MkdirAll(filepath.Join(vulnDir, ver), 0700); err != nil {
			return xerrors.Errorf("Failed to create vuln directory. err: %w", err)
		}

		for cveID, def := range defs {
			f, err := os.Create(filepath.Join(vulnDir, ver, fmt.Sprintf("%s.json", cveID)))
			if err != nil {
				return xerrors.Errorf("Failed to create vuln data file. err: %w", err)
			}

			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(def); err != nil {
				_ = f.Close() // ignore error; Write error takes precedence
				return xerrors.Errorf("Failed to encode vuln data. err: %w", err)
			}

			if err := f.Close(); err != nil {
				return xerrors.Errorf("Failed to close vuln data file. err: %w", err)
			}
		}
	}

	log15.Info("Setting Last Updated Date")
	if err := setLastUpdatedDate("goval-dictionary/oracle"); err != nil {
		return xerrors.Errorf("Failed to set last updated date. err: %w", err)
	}

	return nil
}
