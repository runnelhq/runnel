package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/secretstore"
	"github.com/runnelhq/runnel/internal/vars"
	"github.com/runnelhq/runnel/internal/winsvc"
)

type secretOptions struct {
	fromFile string
	base64   bool
}

func newSecretCommand() *cobra.Command {
	var o secretOptions
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage the local secret store of a project",
		Long: "Sets, lists and deletes the secrets of a project in the local secret store (ADR 0022), read by\n" +
			"${secret:NAME} (unless secretStore says otherwise) and ${secret:local/NAME}. The values lie\n" +
			"outside the project, encrypted with AES-256-GCM, in %LOCALAPPDATA%\\runnel\\secrets (elsewhere\n" +
			"<config>/runnel/secrets). The key is " + secretstore.MasterKeyEnv + ": 32 random bytes in base64\n" +
			"(e.g. from \"openssl rand -base64 32\"). Values are never shown.",
		Args: cobra.NoArgs,
	}
	set := &cobra.Command{
		Use:   "set NAME [project-dir]",
		Short: "Set a secret of a project",
		Long: "Sets the secret NAME of the project in project-dir (or the current directory). The value is read\n" +
			"from the console without showing it (Windows), from stdin (one trailing line break is removed) or\n" +
			"with --from-file from a file (as it is); never from the command line.",
		Example: "  runnel secret set erp-password\n" +
			"  runnel secret set sftp-key --from-file id_ed25519\n" +
			"  runnel secret set client-cert --from-file client.pfx --base64",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setSecret(cmd, args[0], dirArg(args[1:]), o, time.Now())
		},
	}
	set.Flags().StringVar(&o.fromFile, "from-file", "", "read the value from this file (e.g. a private key in PEM)")
	set.Flags().BoolVar(&o.base64, "base64", false, "store the value in base64 (for binary files such as PFX)")
	list := &cobra.Command{
		Use:   "list [project-dir]",
		Short: "List the names of the secrets of a project (never their values)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listSecrets(cmd.OutOrStdout(), dirArg(args))
		},
	}
	del := &cobra.Command{
		Use:   "delete NAME [project-dir]",
		Short: "Delete a secret of a project",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteSecret(cmd.OutOrStdout(), args[0], dirArg(args[1:]))
		},
	}
	cmd.AddCommand(set, list, del)
	return cmd
}

// secretProject checks the name of a secret and finds the project.
func secretProject(name, dir string) (string, error) {
	if !vars.ValidSecretName(name) {
		return "", usageError{fmt.Errorf("invalid secret name %q (letters, digits, ., - and _)", name)}
	}
	return findProject(dir)
}

func setSecret(cmd *cobra.Command, name, dir string, o secretOptions, now time.Time) error {
	root, err := secretProject(name, dir)
	if err != nil {
		return err
	}
	store, err := secretstore.User()
	if err != nil {
		return err
	}
	value, err := readSecretValue(cmd, name, o)
	if err != nil {
		return err
	}
	if err := store.Set(root, name, value, now); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Secret %s is set for the project %s (local store of the user).\n", name, projectName(root))
	return nil
}

// readSecretValue reads the value of a secret from --from-file, the console or stdin.
func readSecretValue(cmd *cobra.Command, name string, o secretOptions) (string, error) {
	var data []byte
	switch in := cmd.InOrStdin(); {
	case o.fromFile != "":
		info, err := os.Stat(o.fromFile)
		if err != nil {
			return "", err
		}
		if info.Size() > secretstore.MaxValue {
			return "", fmt.Errorf("%s is larger than %d MB", o.fromFile, secretstore.MaxValue>>20)
		}
		if data, err = os.ReadFile(o.fromFile); err != nil {
			return "", err
		}
	case isTerminal(in):
		v, err := winsvc.ReadPassword(cmd.ErrOrStderr(), in, "Value of "+name+": ")
		if errors.Is(err, winsvc.ErrNotSupported) {
			return "", errors.New("cannot read the value from the terminal without showing it on this system; pass it on stdin (e.g. from a file with <) or use --from-file")
		}
		if err != nil {
			return "", err
		}
		data = []byte(v)
	default:
		var err error
		if data, err = io.ReadAll(io.LimitReader(in, secretstore.MaxValue+2)); err != nil {
			return "", err
		}
		data = []byte(strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"))
	}
	if o.base64 {
		data = []byte(base64.StdEncoding.EncodeToString(data))
	}
	switch {
	case len(data) == 0:
		return "", errors.New("the value is empty")
	case len(data) > secretstore.MaxValue:
		return "", fmt.Errorf("the value is larger than %d MB", secretstore.MaxValue>>20)
	case !utf8.Valid(data):
		return "", errors.New("the value is not text (UTF-8); use --base64 for binary files (e.g. PFX)")
	}
	return string(data), nil
}

func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func listSecrets(out io.Writer, dir string) error {
	root, err := findProject(dir)
	if err != nil {
		return err
	}
	store, err := secretstore.User()
	if err != nil {
		return err
	}
	list, err := store.List(root)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		_, _ = fmt.Fprintf(out, "No secrets for the project %s in the local store of the user.\n", projectName(root))
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tUPDATED")
	for _, s := range list {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Updated.Local().Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func deleteSecret(out io.Writer, name, dir string) error {
	root, err := secretProject(name, dir)
	if err != nil {
		return err
	}
	store, err := secretstore.User()
	if err != nil {
		return err
	}
	deleted, err := store.Delete(root, name)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("secret %s is not set for the project %s (local store of the user)", name, projectName(root))
	}
	_, _ = fmt.Fprintf(out, "Secret %s is deleted.\n", name)
	return nil
}

// projectName names a project folder in messages: relative to the working directory,
// absolute if that is the folder itself.
func projectName(root string) string {
	if p := displayPath(root); p != "." {
		return p
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}
