package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http/httptest"
	"runtime"

	"testing"

	cmds "github.com/fgeth/fg-ipfs-cmds"
	files "github.com/fgeth/fg-ipfs-files"
)

type VersionOutput struct {
	Version string
	Commit  string
	Repo    string
	System  string
	Golang  string
}

type testEnv struct {
	version, commit, repoVersion string
	t                            *testing.T
	wait                         chan struct{}
}

func getCommit(env cmds.Environment) (string, bool) {
	tEnv, ok := env.(testEnv)
	return tEnv.commit, ok
}

func getVersion(env cmds.Environment) (string, bool) {
	tEnv, ok := env.(testEnv)
	return tEnv.version, ok
}

func getRepoVersion(env cmds.Environment) (string, bool) {
	tEnv, ok := env.(testEnv)
	return tEnv.repoVersion, ok
}

func getTestingT(env cmds.Environment) (*testing.T, bool) {
	tEnv, ok := env.(testEnv)
	return tEnv.t, ok
}

func getWaitChan(env cmds.Environment) (chan struct{}, bool) {
	tEnv, ok := env.(testEnv)
	return tEnv.wait, ok

}

var (
	cmdRoot = &cmds.Command{
		Options: []cmds.Option{
			// global options, added to every command
			cmds.OptionEncodingType,
			cmds.OptionStreamChannels,
			cmds.OptionTimeout,
		},

		Subcommands: map[string]*cmds.Command{
			"error": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					return errors.New("an error occurred")
				},
			},
			"lateerror": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					re.Emit("some value")
					return errors.New("an error occurred")
				},
				Type: "",
			},
			"encode": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					return errors.New("an error occurred")
				},
				Type: "",
				Encoders: cmds.EncoderMap{
					cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, v string) error {
						fmt.Fprintln(w, v)
						return nil
					}),
				},
			},
			"lateencode": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					re.Emit("hello")
					return errors.New("an error occurred")
				},
				Type: "",
				Encoders: cmds.EncoderMap{
					cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, v string) error {
						fmt.Fprintln(w, v)
						if v != "hello" {
							return fmt.Errorf("expected hello, got %s", v)
						}
						return nil
					}),
				},
			},
			"protoencode": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					return errors.New("an error occurred")
				},
				Type: "",
				Encoders: cmds.EncoderMap{
					cmds.Protobuf: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, v string) error {
						fmt.Fprintln(w, v)
						return nil
					}),
				},
			},
			"protolateencode": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					re.Emit("hello")
					return errors.New("an error occurred")
				},
				Type: "",
				Encoders: cmds.EncoderMap{
					cmds.Protobuf: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, v string) error {
						fmt.Fprintln(w, v)
						return nil
					}),
				},
			},
			"doubleclose": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					t, ok := getTestingT(env)
					if !ok {
						return errors.New("error getting *testing.T")
					}

					re.Emit("some value")

					err := re.Close()
					if err != nil {
						t.Error("unexpected error closing:", err)
					}

					err = re.Close()
					if err != cmds.ErrClosingClosedEmitter {
						t.Error("expected double close error, got:", err)
					}

					return nil
				},
				Type: "",
			},

			"single": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					t, ok := getTestingT(env)
					if !ok {
						return errors.New("error getting *testing.T")
					}

					wait, ok := getWaitChan(env)
					if !ok {
						return errors.New("error getting wait chan")
					}

					err := cmds.EmitOnce(re, "some value")
					if err != nil {
						t.Error("unexpected emit error:", err)
					}

					err = re.Emit("this should not be emitted")
					if err != cmds.ErrClosedEmitter {
						t.Errorf("expected emit error %q, got: %v", cmds.ErrClosedEmitter, err)
					}

					err = re.Close()
					if err != cmds.ErrClosingClosedEmitter {
						t.Error("expected double close error, got:", err)
					}

					close(wait)

					return nil
				},
				Type: "",
			},

			"reader": {
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					buf := bytes.NewBufferString("the reader call returns a reader.")
					return re.Emit(buf)
				},
			},

			"echo": {
				Arguments: []cmds.Argument{
					cmds.FileArg("file", true, false, "a file"),
				},
				Type: "",
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					err := re.Emit("i received:")
					if err != nil {
						return err
					}

					it := req.Files.Entries()
					if !it.Next() {
						return it.Err()
					}

					data, err := ioutil.ReadAll(files.FileFromEntry(it))
					if err != nil {
						return err
					}

					return re.Emit(string(data))
				},
			},

			"version": {
				Helptext: cmds.HelpText{
					Tagline:          "Show ipfs version information.",
					ShortDescription: "Returns the current version of ipfs and exits.",
				},
				Type: VersionOutput{},
				Options: []cmds.Option{
					cmds.BoolOption("number", "n", "Only show the version number."),
					cmds.BoolOption("commit", "Show the commit hash."),
					cmds.BoolOption("repo", "Show repo version."),
					cmds.BoolOption("all", "Show all version information"),
				},
				Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
					version, ok := getVersion(env)
					if !ok {
						return cmds.Errorf(cmds.ErrNormal, "couldn't get version")
					}

					repoVersion, ok := getRepoVersion(env)
					if !ok {
						return cmds.Errorf(cmds.ErrNormal, "couldn't get repo version")
					}

					commit, ok := getCommit(env)
					if !ok {
						return cmds.Errorf(cmds.ErrNormal, "couldn't get commit info")
					}

					re.Emit(&VersionOutput{
						Version: version,
						Commit:  commit,
						Repo:    repoVersion,
						System:  runtime.GOARCH + "/" + runtime.GOOS, //TODO: Precise version here
						Golang:  runtime.Version(),
					})
					return nil
				},
				Encoders: cmds.EncoderMap{
					cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, v *VersionOutput) error {

						if repo, ok := req.Options["repo"].(bool); ok && repo {
							_, err := fmt.Fprintf(w, "%v\n", v.Repo)
							return err
						}

						var commitTxt string
						if commit, ok := req.Options["commit"].(bool); ok && commit {
							commitTxt = "-" + v.Commit
						}

						if number, ok := req.Options["number"].(bool); ok && number {
							_, err := fmt.Fprintf(w, "%v%v\n", v.Version, commitTxt)
							return err
						}

						if all, ok := req.Options["all"].(bool); ok && all {
							_, err := fmt.Fprintf(w, "go-ipfs version: %s-%s\n"+
								"Repo version: %s\nSystem version: %s\nGolang version: %s\n",
								v.Version, v.Commit, v.Repo, v.System, v.Golang)

							return err
						}

						_, err := fmt.Fprintf(w, "ipfs version %s%s\n", v.Version, commitTxt)
						return err
					}),
				},
			},
		},
	}
)

func getTestServer(t *testing.T, origins []string, allowGet bool) (cmds.Environment, *httptest.Server) {
	if len(origins) == 0 {
		origins = defaultOrigins
	}

	env := testEnv{
		version:     "0.1.2",
		commit:      "c0mm17", // yes, I know there's no 'm' in hex.
		repoVersion: "4",
		t:           t,
		wait:        make(chan struct{}),
	}

	srvCfg := originCfg(origins)
	srvCfg.AllowGet = allowGet

	return env, httptest.NewServer(NewHandler(env, cmdRoot, srvCfg))
}

func errEq(err1, err2 error) bool {
	if err1 == nil && err2 == nil {
		return true
	}

	if err1 == nil || err2 == nil {
		return false
	}

	return err1.Error() == err2.Error()
}
