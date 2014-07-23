package main

import (
	"bufio"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"code.google.com/p/goauth2/oauth"
	"github.com/mattn/go-scan"
)

// oauth configuration
var config = &oauth.Config{
	ClientId:     "16958ad0bd36ae8",
	ClientSecret: "40f37038e13285da76657c73a22d9840b9dae393",
	AuthURL:      "https://api.imgur.com/oauth2/authorize",
	TokenURL:     "https://api.imgur.com/oauth2/token",
}

func osUserCacheDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".cache")
}

func tokenCacheFile(config *oauth.Config) string {
	hash := fnv.New32a()
	hash.Write([]byte(config.ClientId))
	hash.Write([]byte(config.ClientSecret))
	hash.Write([]byte(config.Scope))
	fn := fmt.Sprintf("imgur-api-tok%v", hash.Sum32())
	return filepath.Join(osUserCacheDir(), url.QueryEscape(fn))
}

func tokenFromFile(file string) (*oauth.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := new(oauth.Token)
	err = gob.NewDecoder(f).Decode(t)
	return t, err
}

func saveToken(file string, token *oauth.Token) error {
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("Warning: failed to cache oauth token: %v", err)
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(token)
}

func getOAuthClient(config *oauth.Config) (*http.Client, error) {
	cacheFile := tokenCacheFile(config)
	token, err := tokenFromFile(cacheFile)
	if err != nil {
		if token, err = tokenFromWeb(config); err != nil {
			return nil, err
		}
		if err = saveToken(cacheFile, token); err != nil {
			return nil, err
		}
	}

	return (&oauth.Transport{
		Token:     token,
		Config:    config,
		Transport: http.DefaultTransport,
	}).Client(), nil
}

func tokenFromWeb(config *oauth.Config) (*oauth.Token, error) {
	config.RedirectURL = ""
	authUrl := config.AuthCodeURL("")
	u2, err := url.Parse(authUrl)
	if err != nil {
		return nil, fmt.Errorf("Parse error: %v", err)
	}
	v := u2.Query()
	v.Set("response_type", "pin")
	u2.RawQuery = v.Encode()
	authUrl = u2.String()

	go openUrl(authUrl)

	fmt.Print("PIN: ")
	b, _, err := bufio.NewReader(os.Stdin).ReadLine()
	if err != nil {
		return nil, fmt.Errorf("Canceled")
	}

	v = url.Values{
		"grant_type":    {"pin"},
		"pin":           {strings.TrimSpace(string(b))},
		"client_id":     {config.ClientId},
		"client_secret": {config.ClientSecret},
	}
	r, err := http.DefaultClient.Post(
		config.TokenURL,
		"application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("Token exchange error: %v", err)
	}
	defer r.Body.Close()

	var res struct {
		Access    string        `json:"access_token"`
		Refresh   string        `json:"refresh_token"`
		ExpiresIn time.Duration `json:"expires_in"`
		Id        string        `json:"id_token"`
	}
	if err = json.NewDecoder(r.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("Token exchange error: %v", err)
	}

	return &oauth.Token{
		AccessToken:  res.Access,
		RefreshToken: res.Refresh,
		Expiry:       time.Now().Add(res.ExpiresIn),
	}, nil
}

func openUrl(u string) error {
	cmd := "xdg-open"
	args := []string{cmd, u}
	if runtime.GOOS == "windows" {
		cmd = "rundll32.exe"
		args = []string{cmd, "url.dll,FileProtocolHandler", u}
	} else if runtime.GOOS == "darwin" {
		cmd = "open"
		args = []string{cmd, u}
	}
	cmd, err := exec.LookPath(cmd)
	if err != nil {
		return err
	}
	p, err := os.StartProcess(cmd, args, &os.ProcAttr{Dir: "", Files: []*os.File{nil, nil, os.Stderr}})
	if err != nil {
		return err
	}
	defer p.Release()
	return nil
}

func valueOrFileContents(value string, filename string) string {
	if value != "" {
		return value
	}
	slurp, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading %q: %v", filename, err)
	}
	return strings.TrimSpace(string(slurp))
}

func main() {
	b, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	params := url.Values{"image": {base64.StdEncoding.EncodeToString(b)}}

	client, err := getOAuthClient(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "auth:", err.Error())
		os.Exit(1)
	}

	r, err := client.PostForm("https://api.imgur.com/3/image", params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err.Error())
		os.Exit(1)
	}
	if r.StatusCode != 200 {
		var message string
		err = scan.ScanJSON(r.Body, "data/error", &message)
		if err != nil {
			message = r.Status
		}
		fmt.Fprintln(os.Stderr, "post:", message)
		os.Exit(1)
	}
	defer r.Body.Close()

	var link string
	err = scan.ScanJSON(r.Body, "data/link", &link)
	if err != nil {
		fmt.Fprintln(os.Stderr, "post:", err.Error())
		os.Exit(1)
	}
	fmt.Println(link)
}