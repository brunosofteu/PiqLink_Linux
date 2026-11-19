package main

// Linux helpers: folders (XDG), settings, opening files and the browser,
// native file pickers (zenity / kdialog), menu entry.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Settings struct {
	Name      string `json:"name"`
	Lang      string `json:"lang"`
	Dir       string `json:"dir"`
	Confirm   bool   `json:"confirm"`
	DeviceID  string `json:"device_id"`
	MenuAsked bool   `json:"menu_asked"`
	Assisted  bool   `json:"assisted"` // consent to the assisted connection (STUN)
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/tmp"
	}
	return h
}

func configDir() string {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		d = filepath.Join(home(), ".config")
	}
	d = filepath.Join(d, "piqlink")
	os.MkdirAll(d, 0o700)
	return d
}

func cacheDir() string {
	d := os.Getenv("XDG_CACHE_HOME")
	if d == "" {
		d = filepath.Join(home(), ".cache")
	}
	d = filepath.Join(d, "piqlink")
	os.MkdirAll(d, 0o700)
	return d
}

// downloadDir is the XDG download folder (~/Scaricati, ~/Downloads ...).
func downloadDir() string {
	if f, err := os.Open(filepath.Join(home(), ".config", "user-dirs.dirs")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(l, "XDG_DOWNLOAD_DIR=") {
				v := strings.Trim(strings.TrimPrefix(l, "XDG_DOWNLOAD_DIR="), `"`)
				v = strings.ReplaceAll(v, "$HOME", home())
				if v != "" && v != home() {
					return v
				}
			}
		}
	}
	for _, n := range []string{"Downloads", "Scaricati", "Descargas"} {
		if st, err := os.Stat(filepath.Join(home(), n)); err == nil && st.IsDir() {
			return filepath.Join(home(), n)
		}
	}
	return home()
}

func loadSettings() *Settings {
	s := &Settings{Confirm: true}
	if b, err := os.ReadFile(filepath.Join(configDir(), "settings.json")); err == nil {
		json.Unmarshal(b, s)
	}
	if s.DeviceID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		s.DeviceID = hex.EncodeToString(b)
		s.save()
	}
	if s.Lang == "" {
		s.Lang = systemLang()
	}
	return s
}

func (s *Settings) save() {
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(configDir(), "settings.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, filepath.Join(configDir(), "settings.json"))
	}
}

func (s *Settings) saveDir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return filepath.Join(downloadDir(), "PiqLink")
}

func (s *Settings) deviceName() string {
	if s.Name != "" {
		return s.Name
	}
	h, _ := os.Hostname()
	if h == "" || h == "localhost" {
		return "Linux"
	}
	return h
}

func systemLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		v := strings.ToLower(os.Getenv(k))
		if len(v) >= 2 {
			switch v[:2] {
			case "it", "en", "es", "zh":
				return v[:2]
			}
			if v != "c" && v != "posix" && !strings.HasPrefix(v, "c.") {
				return "en"
			}
		}
	}
	return "en"
}

// xdgOpen opens a file or folder with the desktop's default program.
func xdgOpen(path string) {
	for _, c := range [][]string{{"xdg-open", path}, {"gio", "open", path}} {
		if p, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(p, c[1:]...)
			if cmd.Start() == nil {
				go cmd.Wait()
				return
			}
		}
	}
}

// openWindow shows the interface: as an app window when a Chromium-based
// browser is installed, otherwise in a tab of the default browser.
func openWindow(url string) {
	for _, b := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "brave-browser", "microsoft-edge", "vivaldi"} {
		if p, err := exec.LookPath(b); err == nil {
			cmd := exec.Command(p, "--app="+url, "--window-size=1000,720")
			if cmd.Start() == nil {
				go cmd.Wait()
				return
			}
		}
	}
	xdgOpen(url)
}

// pickerKind: which native file dialog is available ("" = none: the page uses
// the browser's own picker).
func pickerKind() string {
	desk := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	_, z := exec.LookPath("zenity")
	_, k := exec.LookPath("kdialog")
	switch {
	case k == nil && strings.Contains(desk, "kde"):
		return "kdialog"
	case z == nil:
		return "zenity"
	case k == nil:
		return "kdialog"
	}
	return ""
}

// pickPaths shows the native dialog. folders: choose folders instead of files.
func pickPaths(folders bool, title string) []string {
	var cmd *exec.Cmd
	switch pickerKind() {
	case "zenity":
		args := []string{"--file-selection", "--title=" + title, "--separator=\n"}
		if folders {
			args = append(args, "--directory")
		} else {
			args = append(args, "--multiple")
		}
		cmd = exec.Command("zenity", args...)
	case "kdialog":
		if folders {
			cmd = exec.Command("kdialog", "--title", title, "--getexistingdirectory", home())
		} else {
			cmd = exec.Command("kdialog", "--title", title, "--getopenfilename", home(), "--multiple", "--separate-output")
		}
	default:
		return nil
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return paths
}

// installMenu adds PiqLink to the applications menu (for this user).
func installMenu(icon []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	share := os.Getenv("XDG_DATA_HOME")
	if share == "" {
		share = filepath.Join(home(), ".local", "share")
	}
	iconDir := filepath.Join(share, "icons", "hicolor", "256x256", "apps")
	appDir := filepath.Join(share, "applications")
	os.MkdirAll(iconDir, 0o755)
	os.MkdirAll(appDir, 0o755)
	if err := os.WriteFile(filepath.Join(iconDir, "piqlink.png"), icon, 0o644); err != nil {
		return err
	}
	q := `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	desktop := "[Desktop Entry]\nType=Application\nName=PiqLink\nGenericName=Direct Transfer\n" +
		"Comment=Files from one device to another, directly\nComment[it]=File da un dispositivo all'altro, direttamente\n" +
		"Exec=" + q + " %F\nIcon=piqlink\nTerminal=false\nCategories=Network;FileTransfer;Utility;\nKeywords=transfer;send;share;files;\n"
	return os.WriteFile(filepath.Join(appDir, "piqlink.desktop"), []byte(desktop), 0o644)
}
