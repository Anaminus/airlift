package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/anaminus/but"
	"github.com/anaminus/rbxauth"
	"github.com/pkg/errors"
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

type Client struct {
	*http.Client
}

func (client *Client) Login(authFile string) (err error) {
	var session []*http.Cookie
	if authFile == "" {
		config := rbxauth.Config{}
		if _, session, err = config.Prompt(""); err != nil {
			return errors.Wrap(err, "prompt")
		}
	} else {
		file, err := os.Open(authFile)
		if err != nil {
			return errors.Wrap(err, "open auth file")
		}
		session, err = rbxauth.ReadCookies(file)
		file.Close()
		if err != nil {
			errors.Wrap(err, "read auth file")
		}
	}
	if findCookie(session, ".ROBLOSECURITY") == nil {
		return errors.New("session cookie not found")
	}
	u, err := url.Parse(cookieDomain)
	client.Jar, _ = cookiejar.New(nil)
	client.Jar.SetCookies(u, session)
	return nil
}

func (client *Client) GetAssetVersions(placeID int64, page int) (versions []AssetVersion, err error) {
	logf("getting page %d of asset %d\n", page, placeID)
	url := fmt.Sprintf(assetVersions, placeID, page)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func (client *Client) GetAssetVersion(version AssetVersion, cb func(AssetVersion, io.Reader) error) error {
	logf("getting version %d (vid %d)\n", version.VersionNumber, version.Id)
	if cb == nil {
		return errors.New("callback required")
	}
	url := fmt.Sprintf(assetVersion, version.Id)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	if err := cb(version, resp.Body); err != nil {
		return errors.Wrap(err, "callback")
	}
	return nil
}

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

func formatFilename(format string, v AssetVersion) string {
	return fmt.Sprintf("place_v%d_id%d_vid%d.rbxl", v.VersionNumber, v.AssetId, v.Id)
}

var placeID int64
var authFile string
var output string
var useGit bool
var useTag bool
var usePipe bool
var filename string
var verbose bool

func log(args ...interface{}) {
	if verbose {
		but.Log(args...)
	}
}

func logf(format string, args ...interface{}) {
	if verbose {
		but.Logf(format, args...)
	}
}

var errContinue = errors.New("continue")

func transformFile(transform *Commander, filename string, r io.Reader) error {
	if transform == nil || !usePipe {
		file, err := os.Create(filepath.Join(output, filename))
		if err != nil {
			return errors.Wrap(err, "create file")
		}
		if _, err := io.Copy(file, r); err != nil {
			file.Close()
			return errors.Wrap(err, "write file")
		}
		err = file.Sync()
		file.Close()
		if err != nil {
			return errors.Wrap(err, "sync file")
		}
	}
	if transform != nil {
		var err error
		if usePipe {
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

func main() {
	flag.Int64Var(&placeID, "id", -1, "ID of asset to retrieve versions of.")
	flag.StringVar(&authFile, "auth", "", "Path to a file containing auth cookies. Prompts for login if empty.")
	flag.StringVar(&output, "output", ".", "The directory to output to.")
	flag.BoolVar(&useGit, "git", true, "Compile version files into a git repository.")
	flag.BoolVar(&useTag, "tag", false, "Tag each commit with the version number.")
	flag.BoolVar(&usePipe, "pipe", false, "Pipe version files into transform command instead of writing.")
	flag.StringVar(&filename, "filename", "place.rbxl", "Format version file names.")
	flag.BoolVar(&verbose, "v", false, "Verbose output.")
	flag.Parse()

	if placeID < 0 {
		but.Fail("must specify -id flag")
	}

	if useGit && findGit() == "" {
		but.Fail("git not installed")
	}

	if output == "" || output == "." {
		var err error
		output, err = os.Getwd()
		but.IfFatal(err, "get working directory")
	} else {
		but.IfFatal(os.MkdirAll(output, 0755))
	}

	client := &Client{Client: &http.Client{}}
	but.IfFatal(client.Login(authFile), "login")

	var versions []AssetVersion
	for page := 1; ; page++ {
		v, err := client.GetAssetVersions(placeID, page)
		if len(v) == 0 {
			break
		}
		but.IfFatalf(err, "get versions of %d (page %d)", placeID, page)
		versions = append(versions, v...)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].VersionNumber < versions[j].VersionNumber
	})

	transformArgs := flag.Args()
	var transform *Commander
	if len(transformArgs) > 0 {
		transform = &Commander{
			Cmd:  transformArgs[0],
			Args: transformArgs[1:],
			Dir:  output,
		}
		logf("found transform command %q\n", transform.Cmd)
	}

	var callback func(AssetVersion, io.Reader) error
	if useGit {
		log("using git")
		git := &Commander{
			Cmd: "git",
			Dir: output,
		}

		but.IfFatal(git.RunArgs("init"), "initialize repository")
		callback = func(v AssetVersion, r io.Reader) error {
			if err := transformFile(transform, filename, r); err != nil {
				return err
			}
			if err := git.RunArgs("add", filename); err != nil {
				return err
			}
			commit := strings.NewReader(commitMessage(v, filename))
			if err := git.PipeArgs(commit, "commit",
				// Read message from stdin.
				"-F", "-",
				// Allow empty commits.
				"--allow-empty",
				// Use asset creation time for commit date.
				"--date", strconv.FormatInt(v.Created.Unix(), 10),
				// Spoof author.
				"--author", "airlift <airlift>",
			); err != nil {
				return err
			}
			if useTag {
				return git.RunArgs("tag", fmt.Sprintf("v%d", v.VersionNumber))
			}
			return nil
		}
	} else {
		callback = func(v AssetVersion, r io.Reader) error {
			filename := formatFilename(filename, v)
			return transformFile(transform, filename, r)
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
