package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

const mpvSocketPath = "/tmp/termix-mpv.sock"

// MpvPlayer controls a persistent mpv subprocess via its JSON IPC interface.
// mpv is started once and kept running; we loadfile new tracks into it.
//
// IPC protocol: newline-delimited JSON.
// Send:  {"command": ["loadfile", "ytdl://abc123"], "request_id": 1}
// Recv:  {"request_id": 1, "error": "success", "data": null}
//        {"event": "end-file", ...}
type MpvPlayer struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	conn    net.Conn
	reqID   atomic.Int64
	replies map[int64]chan response
	rmu     sync.Mutex
}

type mpvCommand struct {
	Command   []any `json:"command"`
	RequestID int64 `json:"request_id"`
}

type response struct {
	RequestID int64  `json:"request_id"`
	Error     string `json:"error"`
	Data      any    `json:"data"`
}

func NewMpvPlayer() *MpvPlayer {
	return &MpvPlayer{
		replies: make(map[int64]chan response),
	}
}

// Start spawns the mpv process and waits for its IPC socket to appear.
func (m *MpvPlayer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil {
		return nil // already running
	}

	os.Remove(mpvSocketPath)

	m.cmd = exec.CommandContext(ctx, "mpv",
		"--no-video",
		"--idle=yes",               // keep running even with no track
		"--input-ipc-server="+mpvSocketPath,
		"--ytdl=yes",               // enable yt-dlp integration
		"--really-quiet",
	)
	if err := m.cmd.Start(); err != nil {
		m.cmd = nil
		return fmt.Errorf("starting mpv: %w", err)
	}

	// Wait for the socket to appear (mpv takes a moment to bind it).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(mpvSocketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	conn, err := net.Dial("unix", mpvSocketPath)
	if err != nil {
		m.cmd.Process.Kill() //nolint
		m.cmd = nil
		return fmt.Errorf("connecting to mpv socket: %w", err)
	}
	m.conn = conn

	// Dispatch incoming messages (events + command replies) in background.
	go m.readLoop()

	return nil
}

// readLoop continuously reads newline-delimited JSON from the mpv socket.
func (m *MpvPlayer) readLoop() {
	scanner := bufio.NewScanner(m.conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var r response
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.RequestID == 0 {
			// This is an event (end-file, property-change, etc.).
			// We'll handle events properly in a future milestone.
			continue
		}
		m.rmu.Lock()
		ch, ok := m.replies[r.RequestID]
		if ok {
			delete(m.replies, r.RequestID)
		}
		m.rmu.Unlock()
		if ok {
			ch <- r
		}
	}
}

// send writes a command and waits for the matching reply.
func (m *MpvPlayer) send(args ...any) (response, error) {
	id := m.reqID.Add(1)
	ch := make(chan response, 1)

	m.rmu.Lock()
	m.replies[id] = ch
	m.rmu.Unlock()

	cmd := mpvCommand{Command: args, RequestID: id}
	data, err := json.Marshal(cmd)
	if err != nil {
		return response{}, err
	}
	data = append(data, '\n')

	m.mu.Lock()
	_, err = m.conn.Write(data)
	m.mu.Unlock()
	if err != nil {
		return response{}, fmt.Errorf("writing to mpv: %w", err)
	}

	select {
	case r := <-ch:
		if r.Error != "success" {
			return r, fmt.Errorf("mpv error: %s", r.Error)
		}
		return r, nil
	case <-time.After(5 * time.Second):
		return response{}, errors.New("mpv: command timed out")
	}
}

// — Player interface —

func (m *MpvPlayer) Play(_ context.Context, item queue.Item) error {
	_, err := m.send("loadfile", item.URI, "replace")
	return err
}

func (m *MpvPlayer) Pause(_ context.Context) error {
	_, err := m.send("set_property", "pause", true)
	return err
}

func (m *MpvPlayer) Resume(_ context.Context) error {
	_, err := m.send("set_property", "pause", false)
	return err
}

func (m *MpvPlayer) Seek(_ context.Context, seconds float64) error {
	_, err := m.send("seek", seconds, "absolute")
	return err
}

func (m *MpvPlayer) SetVolume(_ context.Context, pct int) error {
	_, err := m.send("set_property", "volume", pct)
	return err
}

func (m *MpvPlayer) Position(_ context.Context) (float64, bool, error) {
	r, err := m.send("get_property", "time-pos")
	if err != nil {
		// mpv returns an error when nothing is loaded; treat as 0, not playing.
		return 0, false, nil
	}
	pos, ok := r.Data.(float64)
	return pos, ok, nil
}

func (m *MpvPlayer) Stop(_ context.Context) error {
	_, err := m.send("stop")
	return err
}

// Shutdown kills the mpv process cleanly.
func (m *MpvPlayer) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill() //nolint
		m.cmd.Wait()        //nolint
		m.cmd = nil
	}
	os.Remove(mpvSocketPath)
}
