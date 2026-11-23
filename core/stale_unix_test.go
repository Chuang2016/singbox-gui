//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestParsePpid(t *testing.T) {
	// comm contains a space and parentheses on purpose.
	if got := parsePpid("1234 (sing box (x)) S 42 1234 1234 0 -1 4194304"); got != 42 {
		t.Errorf("parsePpid = %d, want 42", got)
	}
	if got := parsePpid("garbage"); got != 0 {
		t.Errorf("parsePpid(garbage) = %d, want 0", got)
	}
}

// TestMain doubles as the fake sing-box: re-executed with
// SBGUI_FAKE_SINGBOX=1 and a forged argv (see startFakeSingbox),
// the test binary just sleeps. A stock sleeper cannot be used
// for this — it would reject the "run -c <cfg>" arguments and
// exit at once.
func TestMain(m *testing.M) {
	if os.Getenv("SBGUI_FAKE_SINGBOX") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeSingboxArgv is the exact argument vector Manager.Start
// issues, pointed at cfgPath.
func fakeSingboxArgv(cfgPath string) []string {
	return []string{"sing-box", "run", "-c", cfgPath}
}

// startFakeSingbox starts a long-lived process carrying the
// fake sing-box argv, as a child of the calling process.
func startFakeSingbox(t *testing.T, cfgPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Args = fakeSingboxArgv(cfgPath)
	cmd.Env = append(os.Environ(), "SBGUI_FAKE_SINGBOX=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake sing-box: %v", err)
	}
	return cmd
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// A signature-matching process whose parent is still alive
// belongs to that live owner: the sweep must not touch it.
func TestCleanupStaleSingboxSkipsLiveParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc enumeration")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	fake := startFakeSingbox(t, cfg)
	defer func() {
		_ = fake.Process.Kill()
		_ = fake.Wait()
	}()
	m := NewManager(dir) // the constructor itself sweeps
	m.cleanupStaleSingbox()
	if !pidAlive(fake.Process.Pid) {
		t.Fatalf("fake sing-box (pid %d) with a live parent was killed", fake.Process.Pid)
	}
}

// TestOrphanHelper is not a test: it is the intermediary for
// TestCleanupStaleSingboxKillsOrphan. It starts a fake sing-box,
// records its pid, and exits, orphaning the fake.
func TestOrphanHelper(t *testing.T) {
	if os.Getenv("SBGUI_ORPHAN_HELPER") != "1" {
		t.Skip("helper process only")
	}
	cmd := exec.Command(os.Args[0])
	cmd.Args = fakeSingboxArgv(os.Getenv("SBGUI_ORPHAN_CFG"))
	cmd.Env = append(os.Environ(), "SBGUI_FAKE_SINGBOX=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("SBGUI_ORPHAN_PIDFILE"),
		[]byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ppidOf(pid int) int {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return -1
	}
	return parsePpid(string(stat))
}

// A signature-matching process whose parent is dead is a
// leftover from a force-quit run: the sweep must terminate it.
// A signature-matching orphan for a DIFFERENT workDir (someone
// else's config) must survive.
func TestCleanupStaleSingboxKillsOrphan(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc enumeration")
	}
	spawnOrphan := func(cfg string) int {
		pidfile := filepath.Join(t.TempDir(), "pid")
		helper := exec.Command(os.Args[0], "-test.run", "^TestOrphanHelper$")
		helper.Env = append(os.Environ(),
			"SBGUI_ORPHAN_HELPER=1",
			"SBGUI_ORPHAN_CFG="+cfg,
			"SBGUI_ORPHAN_PIDFILE="+pidfile,
		)
		if out, err := helper.CombinedOutput(); err != nil {
			t.Fatalf("orphan helper: %v\n%s", err, out)
		}
		raw, err := os.ReadFile(pidfile)
		if err != nil {
			t.Fatalf("read pidfile: %v", err)
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Fatalf("bad pidfile: %v", err)
		}
		// Wait for the reparent to init.
		deadline := time.Now().Add(3 * time.Second)
		for ppidOf(pid) != 1 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if pp := ppidOf(pid); pp != 1 {
			t.Skipf("orphan reparented to pid %d, not init; environment-specific, skipping", pp)
		}
		return pid
	}

	dir := t.TempDir()
	victim := spawnOrphan(filepath.Join(dir, "config.json"))
	decoy := spawnOrphan(filepath.Join(t.TempDir(), "config.json"))
	defer func() { _ = syscall.Kill(decoy, syscall.SIGKILL) }()

	m := NewManager(dir) // the constructor itself sweeps
	_ = m
	deadline := time.Now().Add(5 * time.Second)
	for pidAlive(victim) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if pidAlive(victim) {
		t.Fatalf("orphaned sing-box (pid %d) survived the sweep", victim)
	}
	if !pidAlive(decoy) {
		t.Fatalf("orphan for a different config (pid %d) was killed", decoy)
	}
}
