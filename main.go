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

func intlen(i int) int {
	n := 1
	if i >= 100000000 {
		n += 8
		i /= 100000000
	}
	if i >= 10000 {
		n += 4
		i /= 10000
	}
	if i >= 100 {
		n += 2
		i /= 100
	}
	if i >= 10 {
		n += 1
	}
	return n
}

func gitter(repo string) (func(...string), func(string, ...string)) {
	return func(args ...string) {
			args = append([]string{"-C", repo}, args...)
			b, err := exec.Command("git", args...).CombinedOutput()
			if err != nil {
				but.Failf("git %s\n%s\n%s", strings.Join(args, " "), string(b), err)
			}
		},
		func(stdin string, args ...string) {
			args = append([]string{"-C", repo}, args...)
			cmd := exec.Command("git", args...)
			cmd.Stdin = strings.NewReader(stdin)
			b, err := cmd.CombinedOutput()
			if err, ok := err.(*exec.ExitError); ok && err.ExitCode() != 1 {
				but.Failf("git %s\n%s\n%s", strings.Join(args, " "), string(b), err)
			}
		}
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

func main() {
	var placeID int64
	var authFile string
	var output string
	var useGit bool
	flag.Int64Var(&placeID, "id", -1, "ID of asset to retrieve versions of.")
	flag.StringVar(&authFile, "auth", "", "Path to a file containing auth cookies. Prompts for login if empty.")
	flag.StringVar(&output, "output", ".", "The directory to output to.")
	flag.BoolVar(&useGit, "git", false, "Compile file versions into a git repository.")
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

	var width int
	for _, version := range versions {
		w := intlen(int(version.VersionNumber))
		if w > width {
			width = w
		}
	}

	var cb func(AssetVersion, io.Reader) error
	if useGit {
		const filename = `place.rbxl`
		git, gitin := gitter(output)
		git("init")
		cb = func(v AssetVersion, r io.Reader) error {
			file, err := os.Create(filepath.Join(output, filename))
			if err != nil {
				return errors.Wrapf(err, "create v%d", v.VersionNumber)
			}
			if _, err := io.Copy(file, r); err != nil {
				file.Close()
				return errors.Wrapf(err, "write v%d", v.VersionNumber)
			}
			if err := file.Sync(); err != nil {
				file.Close()
				return errors.Wrapf(err, "sync v%d", v.VersionNumber)
			}
			file.Close()

			git("add", filename)
			gitin(commitMessage(v, filename), "commit",
				"-F", "-",
				"--date", strconv.FormatInt(v.Created.Unix(), 10),
				"--author", "airlift <airlift>",
			)
			return nil
		}
	} else {
		cb = func(v AssetVersion, r io.Reader) error {
			filename := fmt.Sprintf("place_v%0*d_id%d_vid%d.rbxl", width, v.VersionNumber, v.AssetId, v.Id)
			file, err := os.Create(filepath.Join(output, filename))
			if err != nil {
				return errors.Wrapf(err, "create v%d", v.VersionNumber)
			}
			defer file.Close()
			if _, err := io.Copy(file, r); err != nil {
				return errors.Wrapf(err, "write v%d", v.VersionNumber)
			}
			if err := file.Sync(); err != nil {
				return errors.Wrapf(err, "sync v%d", v.VersionNumber)
			}
			return nil
		}
	}

	for _, version := range versions {
		but.IfFatalf(client.GetAssetVersion(version, cb), "get asset version %d", version.Id)
	}
}
