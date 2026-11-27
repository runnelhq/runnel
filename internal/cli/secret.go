package cli

import (
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/secretstore"
	"github.com/runnelhq/runnel/internal/vars"
	"github.com/runnelhq/runnel/internal/winsvc"
)

type secretOptions struct {
	service  bool
	fromFile string
	base64   bool
}

func newSecretCommand() *cobra.Command {
	var o secretOptions
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage the local secret store of a project",
		Long: "Sets, lists and deletes the secrets of a project in the local secret store (ADR 0022), read by\n" +
			"${secret:NAME} (unless secretStore says otherwise) and ${secret:local/NAME}; missing lists those\n" +
			"the project uses but are not set. The values lie outside the project, encrypted with AES-256-GCM:\n" +
			"for the user in %LOCALAPPDATA%\\runnel\\secrets (elsewhere <config>/runnel/secrets), with --service\n" +
			"for the Windows service in %ProgramData%\\runnel\\secrets (as administrator). On Windows the key is\n" +
			"protected with DPAPI; if " + secretstore.MasterKeyEnv + " is set (32 random bytes in base64), it is the key\n" +
			"instead, as on other systems. Values are never shown.",
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
			"  runnel secret set client-cert --from-file client.pfx --base64\n" +
			"  runnel secret set erp-password D:\\flows --service",
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
			return listSecrets(cmd.OutOrStdout(), dirArg(args), o)
		},
	}
	del := &cobra.Command{
		Use:   "delete NAME [project-dir]",
		Short: "Delete a secret of a project",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteSecret(cmd.OutOrStdout(), args[0], dirArg(args[1:]), o)
		},
	}
	missing := &cobra.Command{
		Use:   "missing [project-dir]",
		Short: "List the secrets the project uses that are not set",
		Long: "Lists every ${secret:…} of the project that is not set in its store, per environment (exit code 1\n" +
			"if there is one, 2 if the project cannot be read). The local store is that of the user, or with\n" +
			"--service that of the Windows service; env is checked in the environment of this console.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return missingSecrets(cmd.OutOrStdout(), dirArg(args), o)
		},
	}
	for _, c := range []*cobra.Command{set, list, del, missing} {
		c.Flags().BoolVar(&o.service, "service", false, "the store of the Windows service instead of the user (needs administrator rights)")
	}
	cmd.AddCommand(set, list, del, missing)
	return cmd
}

func serviceSecrets() *secretstore.Store {
	return &secretstore.Store{Dir: winsvc.SecretsDir(), Machine: true}
}

// secretStore returns the store of the user or (service) of the Windows service, with a
// name for messages.
func secretStore(service bool) (*secretstore.Store, string, error) {
	if !service {
		s, err := secretstore.User()
		return s, "user", err
	}
	if runtime.GOOS != "windows" {
		return nil, "", errors.New("--service: the local secret store of the service exists only on Windows")
	}
	return serviceSecrets(), "service", nil
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
	store, owner, err := secretStore(o.service)
	if err != nil {
		return err
	}
	value, err := readSecretValue(cmd, name, o)
	if err != nil {
		return err
	}
	if o.service {
		if err := winsvc.PrepareSecretsDir(store.Dir); err != nil {
			return fmt.Errorf("%w (run the console as administrator)", err)
		}
	}
	if err := store.Set(root, name, value, now); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("%w (run the console as administrator)", err)
		}
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Secret %s is set for the project %s (local store of the %s).\n", name, projectName(root), owner)
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

func listSecrets(out io.Writer, dir string, o secretOptions) error {
	root, err := findProject(dir)
	if err != nil {
		return err
	}
	store, owner, err := secretStore(o.service)
	if err != nil {
		return err
	}
	list, err := store.List(root)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		_, _ = fmt.Fprintf(out, "No secrets for the project %s in the local store of the %s.\n", projectName(root), owner)
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tUPDATED")
	for _, s := range list {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Updated.Local().Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func deleteSecret(out io.Writer, name, dir string, o secretOptions) error {
	root, err := secretProject(name, dir)
	if err != nil {
		return err
	}
	store, owner, err := secretStore(o.service)
	if err != nil {
		return err
	}
	deleted, err := store.Delete(root, name)
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w (run the console as administrator)", err)
	}
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("secret %s is not set for the project %s (local store of the %s)", name, projectName(root), owner)
	}
	_, _ = fmt.Fprintf(out, "Secret %s is deleted.\n", name)
	return nil
}

// secretRef is a ${secret:…} of the project with its first use.
type secretRef struct {
	store, name, use string
	uses             int
}

func missingSecrets(out io.Writer, dir string, o secretOptions) error {
	root, err := findProject(dir)
	if err != nil {
		return notChecked(err)
	}
	p, err := model.LoadProject(root)
	if err != nil {
		return notChecked(err)
	}
	local, _, err := secretStore(o.service)
	if err != nil {
		return notChecked(err)
	}
	refs, err := projectSecrets(root)
	if err != nil {
		return notChecked(err)
	}
	envs := []string{""}
	if len(p.Environments) > 0 {
		envs = p.EnvironmentNames()
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	var missing int
	for _, r := range refs {
		for _, name := range envs {
			env := p.Environments[name]
			store, state := model.SecretStatus(p, env, r.store, r.name)
			if store == model.SecretStoreLocal {
				state = model.LocalSecretState(p, r.name, local)
			}
			if state == model.SecretSet {
				continue
			}
			if missing++; missing == 1 {
				_, _ = fmt.Fprintln(tw, "NAME\tENVIRONMENT\tSTORE\tSTATE\tUSED IN")
			}
			use := r.use
			if r.uses > 1 {
				use += fmt.Sprintf(" (+%d)", r.uses-1)
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.name, cmp.Or(name, "-"), store, state, use)
		}
	}
	_ = tw.Flush()
	if missing > 0 {
		_, _ = fmt.Fprintf(out, "%s not set.\n", plural(missing, "secret"))
		return errExitCode(1)
	}
	_, _ = fmt.Fprintf(out, "No secrets missing (%s used by the project).\n", plural(len(refs), "secret"))
	return nil
}

// Limits for reading the project in "runnel secret missing".
const (
	maxSecretFiles    = 10000
	maxSecretFileSize = 4 << 20
)

// projectSecrets returns every ${secret:…} in the YAML files of a project (without
// hidden folders and links), sorted by name and store.
func projectSecrets(dir string) ([]*secretRef, error) {
	// Within an os.Root, so that no link swapped in while reading leads out of the project.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	byRef := map[string]*secretRef{}
	files := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(pathpkg.Ext(path))
		if d.IsDir() || (ext != ".yaml" && ext != ".yml") {
			return nil
		}
		if files++; files > maxSecretFiles {
			return fmt.Errorf("the project has more than %d YAML files", maxSecretFiles)
		}
		if info, err := d.Info(); err != nil {
			return err
		} else if info.Size() > maxSecretFileSize {
			return nil // data rather than a file of the project
		}
		data, err := fs.ReadFile(root.FS(), path)
		if err != nil {
			return err
		}
		uses, err := model.ScanSecrets(path, data)
		if err != nil {
			return nil //nolint:nilerr // a broken file has no uses; validate reports it
		}
		for _, u := range uses {
			key := u.Store + "/" + u.Name
			if r := byRef[key]; r != nil {
				r.uses++
				continue
			}
			byRef[key] = &secretRef{store: u.Store, name: u.Name, use: path + ":" + u.Pos.String(), uses: 1}
		}
		return nil
	})
	out := make([]*secretRef, 0, len(byRef))
	for _, r := range byRef {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *secretRef) int { return strings.Compare(a.name+"/"+a.store, b.name+"/"+b.store) })
	return out, err
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
