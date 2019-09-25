package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anaminus/but"
	"github.com/anaminus/rbxauth"
	"github.com/jessevdk/go-flags"
)

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func findGit() string {
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	return git
}

const cookieDomain = `https://www.roblox.com/`
const assetVersions = `https://api.roblox.com/assets/%d/versions?page=%d`
const assetVersion = `https://assetgame.roblox.com/Asset?versionId=%d`

type AssetVersion struct {
	Id                   int64
	AssetId              int64
	VersionNumber        int64
	ParentAssetVersionId int64
	CreatorType          int
	CreatorTargetId      int64
	CreatingUniverseId   *int64
	Created              time.Time
	Updated              time.Time
}

////////////////////////////////////////////////////////////////////////////////

type StatusError interface {
	StatusCode() int
}

// statusError represents an error derived from the status code of an HTTP
// response. It also wraps an API error response.
type statusError struct {
	code int
	resp error
}

// Error implements the error interface.
func (err statusError) Error() string {
	if err.resp == nil {
		return "http " + strconv.Itoa(err.code) + ": " + http.StatusText(err.code)
	}
	return "http " + strconv.Itoa(err.code) + ": " + err.resp.Error()
}

// Unwrap implements the Unwrap interface.
func (err statusError) Unwrap() error {
	return err.resp
}

// StatusCode returns the status code of the error.
func (err statusError) StatusCode() int {
	return err.code
}

// if Status wraps err in a statusError if code is not 2XX, and returns err
// otherwise.
func ifStatus(code int, err error) error {
	if code < 200 || code >= 300 {
		return &statusError{code: code, resp: err}
	}
	return err
}

////////////////////////////////////////////////////////////////////////////////

type Client struct {
	*http.Client
}

func (client *Client) Login(authFile string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("login: %w", err)
		}
	}()

	var session []*http.Cookie
	if authFile == "" {
		stream := rbxauth.StandardStream()
		if _, session, err = stream.Prompt(""); err != nil {
			return err
		}
	} else {
		file, err := os.Open(authFile)
		if err != nil {
			return fmt.Errorf("auth file: %w", err)
		}
		session, err = rbxauth.ReadCookies(file)
		file.Close()
		if err != nil {
			return fmt.Errorf("auth file: %w", err)
		}
	}
	if findCookie(session, ".ROBLOSECURITY") == nil {
		return errors.New("missing session cookie")
	}
	u, err := url.Parse(cookieDomain)
	client.Jar, _ = cookiejar.New(nil)
	client.Jar.SetCookies(u, session)
	return nil
}

func (client *Client) GetAssetVersions(assetID int64, page int) (versions []AssetVersion, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("get versions of asset %d (page %d): %w", assetID, page, err)
		}
	}()
	logf("getting page %d of asset %d\n", page, assetID)
	url := fmt.Sprintf(assetVersions, assetID, page)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := ifStatus(resp.StatusCode, nil); err != nil {
		return nil, err
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func (client *Client) GetAssetVersion(version AssetVersion, cb func(AssetVersion, io.Reader) error) error {
	logf("getting version %d (vid %d)\n", version.VersionNumber, version.Id)
	if cb == nil {
		return errors.New("missing callback")
	}
	url := fmt.Sprintf(assetVersion, version.Id)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := ifStatus(resp.StatusCode, nil); err != nil {
		return err
	}
	if err := cb(version, resp.Body); err != nil {
		return fmt.Errorf("callback: %w", err)
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

type Commander struct {
	Cmd  string
	Args []string
	Dir  string
	Err  func([]string, []byte, error) error
}

func (c *Commander) run(cmd *exec.Cmd) error {
	b, err := cmd.CombinedOutput()
	if err != nil && c.Err != nil {
		return c.Err(cmd.Args, b, err)
	}
	return err
}

func (c *Commander) Run() error {
	cmd := exec.Command(c.Cmd, c.Args...)
	cmd.Dir = c.Dir
	return c.run(cmd)
}

func (c *Commander) Pipe(r io.Reader) error {
	cmd := exec.Command(c.Cmd, c.Args...)
	cmd.Dir = c.Dir
	cmd.Stdin = r
	return c.run(cmd)
}

func (c *Commander) RunArgs(args ...string) error {
	cmd := exec.Command(c.Cmd, args...)
	cmd.Dir = c.Dir
	return c.run(cmd)
}

func (c *Commander) PipeArgs(r io.Reader, args ...string) error {
	cmd := exec.Command(c.Cmd, args...)
	cmd.Dir = c.Dir
	cmd.Stdin = r
	return c.run(cmd)
}

////////////////////////////////////////////////////////////////////////////////

func commitMessage(v AssetVersion, filename string) string {
	const format = "Update %s to version %d\n\n" +
		"Id: %d\n" +
		"AssetId: %d\n" +
		"VersionNumber: %d\n" +
		"ParentAssetVersionId: %d\n" +
		"CreatorType: %d\n" +
		"CreatorTargetId: %d\n" +
		"Created: %s\n" +
		"Updated: %s\n"
	return fmt.Sprintf(format,
		filename,
		v.VersionNumber,
		v.Id,
		v.AssetId,
		v.VersionNumber,
		v.ParentAssetVersionId,
		v.CreatorType,
		v.CreatorTargetId,
		v.Created.Format("Mon, 02 Jan 2006 15:04:05 +0700"),
		v.Updated.Format("Mon, 02 Jan 2006 15:04:05 +0700"),
	)
}

func selectVersionField(v AssetVersion, field string) string {
	switch strings.ToLower(field) {
	case "id", "vid":
		return strconv.FormatInt(v.Id, 10)
	case "assetid", "aid":
		return strconv.FormatInt(v.AssetId, 10)
	case "versionnumber", "v":
		return strconv.FormatInt(v.VersionNumber, 10)
	case "parentassetversionid", "pid":
		return strconv.FormatInt(v.ParentAssetVersionId, 10)
	case "creatortype", "ct":
		return strconv.FormatInt(int64(v.CreatorType), 10)
	case "creatortargetid", "cid":
		return strconv.FormatInt(v.CreatorTargetId, 10)
	case "creatinguniverseid":
		if v.CreatingUniverseId != nil {
			return strconv.FormatInt(*v.CreatingUniverseId, 10)
		}
	case "created", "t":
		return v.Created.UTC().Format("2006-01-02-15-04-05")
	case "updated", "u":
		return v.Updated.UTC().Format("2006-01-02-15-04-05")
	}
	return ""
}

func formatFilename(format string, v AssetVersion) string {
	var buf strings.Builder
	i := 0
	for j := 0; j < len(format); j++ {
		if format[j] == '%' && j+1 < len(format) {
			buf.WriteString(format[i:j])
			j++
			i = j
			if format[j] == '%' {
				// buf.WriteByte(format[j])
				continue
			}
			for ; j < len(format); j++ {
				if c := format[j]; '0' <= c && c <= '9' ||
					'a' <= c && c <= 'z' ||
					'A' <= c && c <= 'Z' {
					continue
				}
				break
			}
			if j == i {
				buf.WriteByte(format[i-1])
			} else {
				buf.WriteString(selectVersionField(v, format[i:j]))
			}
			i = j
		}
	}
	buf.WriteString(format[i:])
	return buf.String()
}

////////////////////////////////////////////////////////////////////////////////

const Usage = `-i ASSET [options] [transform [args...]]

Downloads versions of Roblox assets to a Git repository.

Any unprocessed arguments are interpreted as a command with arguments, which can
be used to transform files. This command runs with --output as the working
directory, and runs after each version is downloaded. If the command fails, then
that version is skipped. If --git is enabled, then the entire working tree is
committed after the command succeeds.

The --filename format may contain variables of the form %VARIABLE that expand
based on data from the version currently being processed. %% emits a literal %
character, and unknown variables emit empty strings. Variables are
case-insensitive.

  Variable              Alias  Description
  ------------------------------------------------------------------
  Id                    vid    Asset version ID.
  AssetId               aid    Asset ID.
  VersionNumber         v      Current version number.
  ParentAssetVersionId  pid    ID of the parent or previous version.
  CreatorTargetId       cid    ID of the asset creator.
  CreatorType           ct     Number indicating the creator type.
  CreatingUniverseId           Universe ID, if present.
  Created               t      When the version was created.
  Updated               u      When the version was last updated.

When --git is disabled, the format must produce names that are unique per
version. If not, "_v%VersionNumber" is appended to the filename, before the file
extension. Using any of the Id, VersionNumber, Created, or Updated variables
will produce unique names.`

var Options = struct {
	AssetID  int64  `long:"id" short:"i"`
	AuthFile string `long:"auth" short:"a"`
	Output   string `long:"output" short:"o"`
	Filename string `long:"filename" short:"f"`
	Git      bool   `long:"git"`
	Tag      bool   `long:"tag"`
	Pipe     bool   `long:"pipe"`
	Verbose  bool   `long:"verbose" short:"v"`
}{0, "", "", "asset.rbxl", true, false, false, false}

var optionData = map[string]*flags.Option{
	"id": &flags.Option{
		Description: `ID of asset to retrieve versions of.`,
		ValueName:   "INTEGER",
		Required:    false,
	},
	"auth": &flags.Option{
		Description: "Path to a file containing authentication " +
			"(.ROBLOSECURITY) cookies. The file is formatted as a number of " +
			"'Set-Cookie' HTTP headers. Prompts the user to login if " +
			"unspecified.",
		ValueName: "PATH",
	},
	"output": &flags.Option{
		Description: "The directory to which files will be written. Defaults " +
			"to the working directory.",
		ValueName: "PATH",
	},
	"filename": &flags.Option{
		Description: "Format the name of written version files.",
		ValueName:   "FORMAT",
		Default:     []string{"asset.rbxl"},
	},
	"git": &flags.Option{
		Description: "Compile version files into a git repository. Set " +
			"--git=false to disable.",
		Default: []string{"true"},
	},
	"tag": &flags.Option{
		Description: "Tag each commit with the version number.",
		Default:     []string{"false"},
	},
	"pipe": &flags.Option{
		Description: "Pipe version files into transform command instead of " +
			"writing.",
		Default: []string{"false"},
	},
	"verbose": &flags.Option{
		Description: "Verbose logging.",
	},
}

func ParseOptions(data interface{}, opts flags.Options) *flags.Parser {
	fp := flags.NewParser(data, opts)
	for name, info := range optionData {
		opt := fp.FindOptionByLongName(name)
		if opt == nil {
			continue
		}
		opt.Description = info.Description
		opt.ValueName = info.ValueName
	}
	return fp
}

////////////////////////////////////////////////////////////////////////////////

func log(args ...interface{}) {
	if Options.Verbose {
		but.Log(args...)
	}
}

func logf(format string, args ...interface{}) {
	if Options.Verbose {
		but.Logf(format, args...)
	}
}

var errContinue = errors.New("continue")

func transformFile(transform *Commander, filename string, r io.Reader) error {
	if transform == nil || !Options.Pipe {
		file, err := os.Create(filepath.Join(Options.Output, filename))
		if err != nil {
			return fmt.Errorf("create file: %w", err)
		}
		if _, err := io.Copy(file, r); err != nil {
			file.Close()
			return fmt.Errorf("write file: %w", err)
		}
		err = file.Sync()
		file.Close()
		if err != nil {
			return fmt.Errorf("sync file: %w", err)
		}
	}
	if transform != nil {
		var err error
		if Options.Pipe {
			err = transform.Pipe(r)
		} else {
			err = transform.Run()
		}
		if err != nil {
			// If the transform command fails, continue on to the next version.
			log("transform: ", err)
			return errContinue
		}
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

func main() {
	fp := ParseOptions(&Options, flags.Default|flags.PassAfterNonOption)
	fp.Usage = Usage
	transformArgs, err := fp.Parse()
	if err != nil {
		return
	}

	if Options.AssetID <= 0 {
		but.Log("must specify --id flag")
		fp.WriteHelp(os.Stderr)
		return
	}

	if Options.Git && findGit() == "" {
		but.Fatal("git not installed")
	}

	if Options.Output == "" || Options.Output == "." {
		var err error
		Options.Output, err = os.Getwd()
		but.IfFatal(err, "get working directory")
	} else {
		but.IfFatal(os.MkdirAll(Options.Output, 0755))
	}

	var versions []AssetVersion
	client := &Client{Client: &http.Client{}}
	for page, authed := 1, false; ; {
		v, err := client.GetAssetVersions(Options.AssetID, page)
		if err != nil && !authed {
			if status := StatusError(nil); errors.As(err, &status) && status.StatusCode() == 403 {
				// Get without auth failed; retry with.
				but.IfFatal(client.Login(Options.AuthFile))
				authed = true
				continue
			}
		}
		but.IfFatal(err)
		if len(v) == 0 {
			break
		}
		versions = append(versions, v...)
		page++
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].VersionNumber < versions[j].VersionNumber
	})

	var transform *Commander
	if len(transformArgs) > 0 {
		transform = &Commander{
			Cmd:  transformArgs[0],
			Args: transformArgs[1:],
			Dir:  Options.Output,
		}
		logf("found transform command %q\n", transform.Cmd)
	}

	var callback func(AssetVersion, io.Reader) error
	if Options.Git {
		log("using git")
		git := &Commander{
			Cmd: "git",
			Dir: Options.Output,
		}

		but.IfFatal(git.RunArgs("init"), "initialize repository")
		callback = func(v AssetVersion, r io.Reader) error {
			if err := transformFile(transform, Options.Filename, r); err != nil {
				return err
			}
			if err := git.RunArgs("add", Options.Filename); err != nil {
				return err
			}
			commit := strings.NewReader(commitMessage(v, Options.Filename))
			if err := git.PipeArgs(commit, "commit",
				// Read message from stdin.
				"-F", "-",
				// Allow empty commits.
				"--allow-empty",
				// Use asset creation time for commit date.
				"--date", strconv.FormatInt(v.Created.Unix(), 10),
			); err != nil {
				return err
			}
			if Options.Tag {
				return git.RunArgs("tag", fmt.Sprintf("v%d", v.VersionNumber))
			}
			return nil
		}
	} else {
		log("using file list")
		// Check whether format will produce unique filenames.
		filename := Options.Filename
		ta := time.Now()
		tb := ta.AddDate(0, 0, 1)
		a := formatFilename(filename, AssetVersion{0, 0, 0, 0, 0, 0, nil, ta, ta})
		b := formatFilename(filename, AssetVersion{1, 0, 1, 0, 0, 0, nil, tb, tb})
		if a == b {
			logf("filename %q not unique\n", filename)
			// If not, append the version number.
			ext := filepath.Ext(filename)
			filename = filename[:len(filename)-len(ext)] + "_v%v" + ext
			logf("using %q as filename\n", filename)
		} else {
			logf("filename %q is unique\n", filename)
		}
		callback = func(v AssetVersion, r io.Reader) error {
			return transformFile(transform, formatFilename(filename, v), r)
		}
	}

	for _, version := range versions {
		err := client.GetAssetVersion(version, callback)
		if err == errContinue {
			continue
		}
		but.IfFatalf(err, "get asset version %d", version.VersionNumber)
	}
}
