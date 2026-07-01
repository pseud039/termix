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

// MpvEvent is what readLoop posts to the Events channel when mpv
// fires an event (as opposed to a command reply).
// The rest of the app only needs to know the type and reason — we
// don't expose raw JSON outside this package.
type MpvEvent struct {
	Type   string // "end-file", "start-file", "pause", "unpause", …
	Reason string // for end-file: "eof" | "stop" | "error" | "quit"
}

// rawEvent is used only inside readLoop to unmarshal mpv event JSON.
// mpv sends: {"event":"end-file","reason":"eof",...}
type rawEvent struct {
	Event  string `json:"event"`
	Reason string `json:"reason"`
}

// MpvPlayer controls a persistent mpv subprocess via its JSON IPC socket.
// mpv is started once and kept running; we loadfile new tracks into it.
//
// IPC protocol: newline-delimited JSON.
//   Send:  {"command": ["loadfile", "ytdl://abc123"], "request_id": 1}
//   Recv:  {"request_id": 1, "error": "success", "data": null}   ← reply
//          {"event": "end-file", "reason": "eof"}                 ← event
type MpvPlayer struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	conn    net.Conn
	reqID   atomic.Int64
	replies map[int64]chan response
	rmu     sync.Mutex

	// events is buffered so readLoop never blocks posting an event even if
	// Bubbletea is briefly busy. 16 slots is way more than we'll ever need.
	events chan MpvEvent
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
		events:  make(chan MpvEvent, 16),
	}
}

// Events returns the channel the app should read to receive mpv events.
// The caller (Bubbletea cmd) blocks on this channel; one event = one read.
func (m *MpvPlayer) Events() <-chan MpvEvent {
	return m.events
}

// Start spawns mpv and connects to its IPC socket.
// Returns a descriptive error if mpv isn't installed or fails to start.
func (m *MpvPlayer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil {
		return nil // already running
	}

	// Fail fast with a clear message if mpv isn't on PATH.
	// exec.LookPath checks PATH so the user knows exactly what's missing.
	mpvBin, err := exec.LookPath("mpv")
	if err != nil {
		return fmt.Errorf("mpv not found in PATH — install mpv to enable playback")
	}

	// Remove any stale socket from a previous crash.
	os.Remove(mpvSocketPath)

	m.cmd = exec.CommandContext(ctx, mpvBin,
		"--no-video",
		"--idle=yes",                          // stay alive with no track loaded
		"--input-ipc-server="+mpvSocketPath,
		"--ytdl=yes",                          // pass YouTube/SoundCloud URLs to yt-dlp
		"--really-quiet",                      // suppress mpv's own terminal output
	)

	if err := m.cmd.Start(); err != nil {
		m.cmd = nil
		return fmt.Errorf("starting mpv: %w", err)
	}

	// mpv binds the socket slightly after launch — poll until it appears.
	// 5 seconds is generous; it usually appears in < 200ms.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(mpvSocketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(mpvSocketPath); err != nil {
		m.cmd.Process.Kill() //nolint
		m.cmd = nil
		return fmt.Errorf("mpv IPC socket never appeared at %s", mpvSocketPath)
	}

	conn, err := net.Dial("unix", mpvSocketPath)
	if err != nil {
		m.cmd.Process.Kill() //nolint
		m.cmd = nil
		return fmt.Errorf("connecting to mpv socket: %w", err)
	}
	m.conn = conn

	go m.readLoop()

	return nil
}

// readLoop continuously reads newline-delimited JSON from the mpv socket
// and dispatches each message to either the reply map or the events channel.
func (m *MpvPlayer) readLoop() {
	scanner := bufio.NewScanner(m.conn)
	for scanner.Scan() {
		line := scanner.Bytes()

		// Every mpv message is valid JSON. Unmarshal into response first —
		// if RequestID is non-zero it's a command reply, otherwise it's an event.
		var r response
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}

		if r.RequestID != 0 {
			// Command reply — wake up whichever send() call is waiting for this ID.
			m.rmu.Lock()
			ch, ok := m.replies[r.RequestID]
			if ok {
				delete(m.replies, r.RequestID)
			}
			m.rmu.Unlock()
			if ok {
				ch <- r
			}
			continue
		}

		// RequestID == 0 → this is an event. Re-unmarshal to get event fields.
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil || ev.Event == "" {
			continue
		}

		// We only forward events the app cares about. Adding more here later
		// (e.g. "pause", "unpause", "metadata-update") is straightforward.
		switch ev.Event {
		case "end-file", "start-file":
			// Non-blocking send: if the channel is somehow full (shouldn't happen
			// with 16 slots), drop the event rather than deadlocking readLoop.
			select {
			case m.events <- MpvEvent{Type: ev.Event, Reason: ev.Reason}:
			default:
			}
		}
	}
	// Scanner stopped — mpv connection closed. The Shutdown() caller handles cleanup.
}

// send writes a command to mpv and blocks until the reply arrives or times out.
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
		// Clean up the pending reply slot so we don't leak.
		m.rmu.Lock()
		delete(m.replies, id)
		m.rmu.Unlock()
		return response{}, errors.New("mpv: command timed out")
	}
}

// ── Player interface ───────────────────────────────────────────────────────

func (m *MpvPlayer) Play(_ context.Context, item queue.Item) error {
	// "replace" = stop current track and immediately start this one.
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
	// "absolute" = seek to position in seconds from start of track.
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
		// mpv returns an error for this property when idle (no track loaded).
		// That's expected — treat as position=0, not playing.
		return 0, false, nil
	}
	pos, ok := r.Data.(float64)
	// ok=false means the JSON value was null (track just ended / not yet started).
	return pos, ok, nil
}

func (m *MpvPlayer) Stop(_ context.Context) error {
	_, err := m.send("stop")
	return err
}

// Shutdown kills mpv and cleans up the socket. Call this on app exit.
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
