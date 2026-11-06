// Package releaseversion computes and validates repository release metadata.
package releaseversion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Intent struct {
	Stage string `json:"stage"`
	Kind  string `json:"kind,omitempty"`
}
type Entry struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Kind    string `json:"kind,omitempty"`
	Digest  string `json:"digest"`
}

func Calculate(previous Entry, intent Intent) (string, error) {
	ranks := map[string]int{"development": 1, "alpha": 2, "stable": 3}
	rank := ranks[intent.Stage]
	if rank == 0 || (intent.Kind != "" && intent.Kind != "fix" && intent.Kind != "addition" && intent.Kind != "breaking") || (rank > 1 && intent.Kind == "") {
		return "", fmt.Errorf("invalid stage or change kind")
	}
	parts := []int{0, 0, 0}
	if previous.Version != "" {
		values := strings.Split(previous.Version, ".")
		if len(values) != 3 {
			return "", fmt.Errorf("invalid previous version")
		}
		for i, value := range values {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || strconv.Itoa(n) != value {
				return "", fmt.Errorf("invalid previous version")
			}
			parts[i] = n
		}
		if ranks[previous.Stage] == 0 || rank < ranks[previous.Stage] {
			return "", fmt.Errorf("invalid stage transition")
		}
		if (previous.Stage == "development" && (parts[0] != 0 || parts[1] != 0 || parts[2] == 0)) || (previous.Stage == "alpha" && (parts[0] != 0 || parts[1] == 0)) || (previous.Stage == "stable" && parts[0] == 0) {
			return "", fmt.Errorf("previous version disagrees with stage")
		}
	}
	if rank > ranks[previous.Stage] && rank > 1 {
		if rank == 2 {
			return "0.1.0", nil
		}
		return "1.0.0", nil
	}
	switch {
	case rank == 1:
		parts[0], parts[1], parts[2] = 0, 0, parts[2]+1
	case rank == 3 && intent.Kind == "breaking":
		parts[0]++
		parts[1], parts[2] = 0, 0
	case intent.Kind == "addition" || intent.Kind == "breaking":
		parts[1]++
		parts[2] = 0
	default:
		parts[2]++
	}
	return fmt.Sprintf("%d.%d.%d", parts[0], parts[1], parts[2]), nil
}

func git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func Digest() (string, error) {
	files, err := git("ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, record := range bytes.Split(files, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		meta, path, ok := strings.Cut(string(record), "\t")
		if !ok {
			return "", fmt.Errorf("invalid index record")
		}
		if path == "VERSION" || path == "version-log.json" {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		mode := "100644"
		var data []byte
		if info.Mode()&os.ModeSymlink != 0 {
			mode = "120000"
			target, e := os.Readlink(path)
			data, err = []byte(target), e
		} else {
			if info.Mode()&0111 != 0 {
				mode = "100755"
			}
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(meta, " 0") {
			return "", fmt.Errorf("unmerged index: %s", path)
		}
		fmt.Fprintf(h, "%d:%s%d:%s%d:", len(path), path, len(mode), mode, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// Run uses an explicit prior-main snapshot. Set always reconstructs one release.
func Run(operation, base string) (Entry, error) {
	var empty Entry
	if _, err := git("rev-parse", "--verify", base+"^{commit}"); err != nil {
		return empty, err
	}
	var history []Entry
	data, err := git("show", base+":version-log.json")
	if err != nil {
		paths, e := git("ls-tree", "--name-only", base, "version-log.json")
		if e != nil || len(paths) != 0 {
			return empty, fmt.Errorf("cannot read base log: %w", err)
		}
	} else if err = json.Unmarshal(data, &history); err != nil || len(history) == 0 {
		return empty, fmt.Errorf("invalid base log")
	}
	var previous Entry
	for _, entry := range history {
		v, e := Calculate(previous, Intent{entry.Stage, entry.Kind})
		decoded, digestErr := hex.DecodeString(entry.Digest)
		if e != nil || v != entry.Version || digestErr != nil || len(decoded) != sha256.Size {
			return empty, fmt.Errorf("corrupt base history")
		}
		previous = entry
	}
	var intent Intent
	if err := readJSON("release-stage.json", &intent); err != nil {
		return empty, err
	}
	version, err := Calculate(previous, intent)
	if err != nil {
		return empty, err
	}
	digest, err := Digest()
	if err != nil {
		return empty, err
	}
	entry := Entry{version, intent.Stage, intent.Kind, digest}
	expected := append(history, entry)
	encoded, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return empty, err
	}
	encoded = append(encoded, '\n')
	switch operation {
	case "set":
		if err := os.WriteFile("VERSION", []byte(version+"\n"), 0600); err != nil {
			return empty, err
		}
		if err := os.WriteFile("version-log.json", encoded, 0600); err != nil {
			return empty, err
		}
	case "check", "current":
		var actual []Entry
		if err := readJSON("version-log.json", &actual); err != nil {
			return empty, err
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(expected)
		v, err := os.ReadFile("VERSION")
		if err != nil || string(v) != version+"\n" || !bytes.Equal(a, b) {
			return empty, fmt.Errorf("release metadata differs from computed release; fetch main and run scripts/set-version")
		}
	default:
		return empty, fmt.Errorf("unknown operation")
	}
	return entry, nil
}
