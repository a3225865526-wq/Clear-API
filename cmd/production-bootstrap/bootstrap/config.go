package bootstrap

import "flag"

// Config carries the parsed command line inputs. No secret may ever be passed
// as a flag value: the administrator password is read from --admin-password-file.
type Config struct {
	Execute           bool
	VerifyOnly        bool
	AdminUsername     string
	AdminDisplayName  string
	AdminPasswordFile string
	TermsFile         string
	PrivacyFile       string
	TermsVersion      string
	PrivacyVersion    string
	ApprovedBy        string
	AdminInitialQuota int
	SelfUseMode       bool
	DemoSite          bool
	CandidateCommit   string
}

// RegisterFlags registers the bootstrap flags on the shared flag set. The
// server's common.InitEnv() later calls flag.Parse() on the same set, so both
// flag groups are parsed in one pass.
func RegisterFlags() *Config {
	cfg := &Config{}
	flag.BoolVar(&cfg.Execute, "execute", false, "run the bootstrap writes (without this flag the command is a read-only dry-run)")
	flag.BoolVar(&cfg.VerifyOnly, "verify-only", false, "verify an already bootstrapped deployment without writing anything")
	flag.StringVar(&cfg.AdminUsername, "admin-username", "", "username of the first root administrator (required on a fresh database)")
	flag.StringVar(&cfg.AdminDisplayName, "admin-display-name", "Root User", "display name of the first root administrator")
	flag.StringVar(&cfg.AdminPasswordFile, "admin-password-file", "", "file containing the administrator password (required on a fresh database; never passed via argv or environment)")
	flag.StringVar(&cfg.TermsFile, "terms-file", "", "UTF-8 file containing the user agreement content")
	flag.StringVar(&cfg.PrivacyFile, "privacy-file", "", "UTF-8 file containing the privacy policy content")
	flag.StringVar(&cfg.TermsVersion, "terms-version", "", "operator-managed version label of the user agreement")
	flag.StringVar(&cfg.PrivacyVersion, "privacy-version", "", "operator-managed version label of the privacy policy")
	flag.StringVar(&cfg.ApprovedBy, "approved-by", "", "operator attestation reference recorded in the legal approval manifest")
	flag.IntVar(&cfg.AdminInitialQuota, "admin-initial-quota", 0, "initial quota of the first administrator (default 0; must be explicitly set when a non-zero balance is required)")
	flag.BoolVar(&cfg.SelfUseMode, "self-use-mode", false, "enable self-use mode (production default: false)")
	flag.BoolVar(&cfg.DemoSite, "demo-site", false, "enable demo site mode (production default: false)")
	flag.StringVar(&cfg.CandidateCommit, "candidate-commit", "", "source commit of the bootstrap candidate recorded in the audit records")
	return cfg
}
