package playground

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
)

const maxFrame = 1 << 20

type nativeResponse struct {
	mu            sync.Mutex
	header        http.Header
	status        int
	stream, raw   bool
	decoder       pg.Decoder
	pending, data bytes.Buffer
	event         string
	total         int
	err           error
	emit          func(pg.Event) error
	cancel        context.CancelFunc
	closed        chan bool
}

func (w *nativeResponse) Header() http.Header { return w.header }
func (w *nativeResponse) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}
func (w *nativeResponse) Flush()                   {}
func (w *nativeResponse) CloseNotify() <-chan bool { return w.closed }
func (w *nativeResponse) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if w.status == 0 {
		w.status = 200
	}
	if w.status < 200 || w.status >= 300 {
		return len(b), nil
	}
	w.total += len(b)
	if w.total > 8<<20 {
		w.err = errors.New("native output exceeds bound")
	} else if w.stream {
		w.err = w.feed(b)
	} else {
		w.pending.Write(b)
	}
	if w.err != nil {
		w.cancel()
		return 0, w.err
	}
	return len(b), nil
}
func (w *nativeResponse) feed(b []byte) error {
	for len(b) > 0 {
		line, tail, ok := bytes.Cut(b, []byte{'\n'})
		if w.pending.Len()+len(line) > maxFrame {
			return errors.New("native frame exceeds bound")
		}
		w.pending.Write(line)
		if !ok {
			break
		}
		b = tail
		s := strings.TrimSuffix(w.pending.String(), "\r")
		w.pending.Reset()
		if s == "" {
			if w.data.Len() > 0 {
				data := strings.TrimSuffix(w.data.String(), "\n")
				ev, err := w.decoder.Frame(w.event, data)
				if err != nil {
					return err
				}
				for _, e := range ev {
					if err = w.emit(e); err != nil {
						return err
					}
				}
				if w.raw {
					if err = w.emit(pg.Event{Name: "raw_event", Data: map[string]any{"protocol": w.decoder.Protocol, "frame": pg.RawFrame(data)}}); err != nil {
						return err
					}
				}
			}
			w.data.Reset()
			w.event = ""
			continue
		}
		if v, ok := strings.CutPrefix(s, "event:"); ok {
			w.event = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(s, "data:"); ok {
			if w.data.Len()+len(v) > maxFrame {
				return errors.New("native frame exceeds bound")
			}
			w.data.WriteString(strings.TrimPrefix(v, " "))
			w.data.WriteByte('\n')
		}
	}
	return nil
}
