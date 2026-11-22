package dictate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Asset is one file the enable switch downloads. The SHA256 is pinned here, so a changed or swapped file is refused.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Unzip names the folder inside a zip whose files are kept, flat. Empty means the file is kept as it is.
	Unzip string `json:"-"`
}

// Plan is what enabling would download and from where, shown to the operator before anything is fetched.
type Plan struct {
	Supported bool    `json:"supported"`
	Reason    string  `json:"reason,omitempty"`
	Source    string  `json:"source"`
	Total     int64   `json:"total"`
	Assets    []Asset `json:"assets"`
	Dir       string  `json:"dir"`
}

// The pinned release. whisper-bin-x64.zip is the CPU build of whisper.cpp, so a GPU machine points the two paths at
// its own build instead. The model is pinned to a commit of its repository, never to main.
var pinned = []Asset{
	{
		Name:   "whisper-bin-x64.zip",
		URL:    "https://github.com/ggml-org/whisper.cpp/releases/download/b5907/whisper-bin-x64.zip",
		SHA256: "ecca07b277d943e9b9f4712856a144b857271a6b333104e511e1988e80db582b",
		Size:   9168428,
		Unzip:  "Release",
	},
	{
		Name:   "ggml-small.en-q5_1.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/ggml-small.en-q5_1.bin",
		SHA256: "bfdff4894dcb76bbf647d56263ea2a96645423f1669176f4844a1bf8e478ad30",
		Size:   190098681,
	},
}

const pinnedSource = "github.com/ggml-org/whisper.cpp (release b5907) and huggingface.co/ggerganov/whisper.cpp"

// Installer downloads the pinned whisper-server and model into Dir, one run at a time, and reports where it is.
type Installer struct {
	Dir string
	// Assets replaces the pinned list. For a test.
	Assets []Asset
	// Done is told the two paths once every file is in place and verified.
	Done func(bin, model string)

	mu    sync.Mutex
	state string
	note  string
	got   int64
}

// Status is the installer as the board reads it. State is idle, working, ready or failed.
type Status struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
	Got   int64  `json:"got"`
}

func (i *Installer) assets() []Asset {
	if i.Assets != nil {
		return i.Assets
	}
	return pinned
}

// Plan says what enabling downloads, or why this system cannot.
func (i *Installer) Plan() Plan {
	p := Plan{Source: pinnedSource, Assets: i.assets(), Dir: i.Dir, Supported: true}
	for _, a := range p.Assets {
		p.Total += a.Size
	}
	if i.Assets == nil && (runtime.GOOS != "windows" || runtime.GOARCH != "amd64") {
		p.Supported = false
		p.Reason = "there is no prebuilt whisper-server for this system. Build whisper.cpp and set the two paths"
	}
	return p
}

// Status is the state of the last run.
func (i *Installer) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == "" {
		return Status{State: "idle"}
	}
	return Status{State: i.state, Note: i.note, Got: i.got}
}

// Start begins the download in the background. It refuses while one is running or when the system is unsupported.
func (i *Installer) Start() error {
	if p := i.Plan(); !p.Supported {
		return errors.New(p.Reason)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == "working" {
		return errors.New("the download is already running")
	}
	i.state, i.note, i.got = "working", "starting", 0
	go i.run()
	return nil
}

func (i *Installer) run() {
	bin, model, err := i.install(context.Background())
	i.mu.Lock()
	if err != nil {
		i.state, i.note = "failed", err.Error()
		i.mu.Unlock()
		return
	}
	i.state, i.note = "ready", ""
	i.mu.Unlock()
	if i.Done != nil {
		i.Done(bin, model)
	}
}

func (i *Installer) progress(note string, got int64) {
	i.mu.Lock()
	i.note, i.got = note, got
	i.mu.Unlock()
}

func (i *Installer) install(ctx context.Context) (bin, model string, err error) {
	var done int64
	for _, a := range i.assets() {
		dst := filepath.Join(i.Dir, "download", a.Name)
		if err := i.fetch(ctx, a, dst, done); err != nil {
			return "", "", fmt.Errorf("%s: %w", a.Name, err)
		}
		done += a.Size
		if a.Unzip != "" {
			if bin, err = unzipFlat(dst, a.Unzip, filepath.Join(i.Dir, "bin")); err != nil {
				return "", "", fmt.Errorf("%s: %w", a.Name, err)
			}
			continue
		}
		keep := filepath.Join(i.Dir, "models", a.Name)
		if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
			return "", "", err
		}
		if err := os.Rename(dst, keep); err != nil {
			return "", "", err
		}
		model = keep
	}
	if bin == "" || model == "" {
		return "", "", errors.New("the plan names no whisper-server or no model")
	}
	return filepath.ToSlash(bin), filepath.ToSlash(model), nil
}

// fetch downloads a to dst, refusing unless the bytes match the pinned size and checksum. A file already there that
// matches is kept.
func (i *Installer) fetch(ctx context.Context, a Asset, dst string, done int64) error {
	if sum, err := fileSum(dst); err == nil && sum == a.SHA256 {
		i.progress("have "+a.Name, done+a.Size)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the server answered %s", res.Status)
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha256.New()
	last := time.Now()
	buf := make([]byte, 256<<10)
	var n int64
	for {
		k, rerr := res.Body.Read(buf)
		if k > 0 {
			if n += int64(k); n > a.Size {
				_ = f.Close()
				_ = os.Remove(part)
				return errors.New("more bytes than the pinned size")
			}
			h.Write(buf[:k])
			if _, err := f.Write(buf[:k]); err != nil {
				_ = f.Close()
				return err
			}
			if time.Since(last) > 300*time.Millisecond {
				i.progress("downloading "+a.Name, done+n)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != a.SHA256 || n != a.Size {
		_ = os.Remove(part)
		return errors.New("the download does not match the pinned checksum, so it was thrown away")
	}
	i.progress("verified "+a.Name, done+a.Size)
	return os.Rename(part, dst)
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// unzipFlat keeps the files directly under folder in the zip, by base name, into out. It answers the whisper-server
// path. Nothing is written outside out, whatever the entries say.
func unzipFlat(zipPath, folder, out string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	server := ""
	for _, e := range zr.File {
		name := strings.ReplaceAll(e.Name, "\\", "/")
		if e.FileInfo().IsDir() || path.Dir(name) != folder || strings.Contains(name, "..") {
			continue
		}
		dst := filepath.Join(out, path.Base(name))
		if err := extract(e, dst); err != nil {
			return "", err
		}
		if strings.HasPrefix(path.Base(name), "whisper-server") {
			server = dst
		}
	}
	if server == "" {
		return "", errors.New("the zip holds no whisper-server")
	}
	return server, nil
}

func extract(e *zip.File, dst string) error {
	rc, err := e.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(rc, 1<<30)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Size is a byte count as the board's confirm sentence says it.
func Size(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", (n+(1<<20)-1)>>20)
}
