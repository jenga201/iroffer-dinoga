package xdcc

import (
	"strings"
	"testing"

	"github.com/jenga201/dinoga/internal/packs"
)

// stubClient records Notices and SendPack calls for assertions.
type stubClient struct {
	notices   []string
	sentPacks []string
	packlist  []packs.Pack
	cancelled bool
}

func (s *stubClient) Notice(target, msg string)  { s.notices = append(s.notices, msg) }
func (s *stubClient) Privmsg(target, msg string) {}
func (s *stubClient) SendPack(target string, p packs.Pack) error {
	s.sentPacks = append(s.sentPacks, p.Name)
	return nil
}
func (s *stubClient) Packlist() []packs.Pack          { return s.packlist }
func (s *stubClient) CancelQueued(target string) bool { return s.cancelled }
func (s *stubClient) PacklistURL() string             { return "" }

func threePacks() []packs.Pack {
	return []packs.Pack{
		{Number: 1, Name: "a.bin", Size: 10},
		{Number: 2, Name: "b.bin", Size: 20},
		{Number: 3, Name: "c.bin", Size: 30},
	}
}

func TestSend(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc send #2")
	if len(c.sentPacks) != 1 || c.sentPacks[0] != "b.bin" {
		t.Fatalf("sentPacks = %v", c.sentPacks)
	}
}

func TestSendOutOfRange(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc send #99")
	if len(c.sentPacks) != 0 {
		t.Fatalf("sentPacks = %v, want none", c.sentPacks)
	}
	if !strings.Contains(strings.Join(c.notices, "\n"), "No such pack") {
		t.Fatalf("notices = %v", c.notices)
	}
}

func TestInfo(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc info #1")
	if len(c.notices) != 1 || !strings.Contains(c.notices[0], "Pack #1: a.bin") {
		t.Fatalf("notices = %v", c.notices)
	}
}

func TestBatch(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc batch #2-#3")
	want := []string{"b.bin", "c.bin"}
	if len(c.sentPacks) != 2 || c.sentPacks[0] != want[0] || c.sentPacks[1] != want[1] {
		t.Fatalf("sentPacks = %v, want %v", c.sentPacks, want)
	}
}

func TestBatchClampsToEnd(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc batch #2-#100")
	if len(c.sentPacks) != 2 {
		t.Fatalf("sentPacks = %v, want 2 (clamped)", c.sentPacks)
	}
}

func TestUnknownCommandNoop(t *testing.T) {
	c := &stubClient{packlist: threePacks()}
	Handle(c, "u", "xdcc somethingelse")
	if len(c.sentPacks) != 0 || len(c.notices) != 0 {
		t.Fatalf("unexpected side effects: sent=%v notices=%v", c.sentPacks, c.notices)
	}
}

func TestRemoveQueued(t *testing.T) {
	c := &stubClient{packlist: threePacks(), cancelled: true}
	Handle(c, "u", "xdcc remove")
	if !strings.Contains(strings.Join(c.notices, "\n"), "Removed queued") {
		t.Fatalf("notices = %v", c.notices)
	}
}

func TestRemoveNothing(t *testing.T) {
	c := &stubClient{packlist: threePacks(), cancelled: false}
	Handle(c, "u", "xdcc cancel")
	if !strings.Contains(strings.Join(c.notices, "\n"), "No queued") {
		t.Fatalf("notices = %v", c.notices)
	}
}

func TestInfoDescription(t *testing.T) {
	pl := threePacks()
	pl[0].Desc = "first pack"
	c := &stubClient{packlist: pl}
	Handle(c, "u", "xdcc info #1")
	if !strings.Contains(strings.Join(c.notices, "\n"), "first pack") {
		t.Fatalf("notices = %v", c.notices)
	}
}
