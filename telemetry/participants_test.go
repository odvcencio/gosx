//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/schema"
)

func participantFixture(t testing.TB) (*Activity[NoFields, int, NoFields], *Telemetry) {
	t.Helper()
	tel, _ := lifecycleOwner(t)
	codec := DomainCodec[int]{Name: "seat", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error { return f.Int("score", int64(v)) }}
	kind, err := newActivityKind(tel, "match", ActivityKindOptions{Roles: []string{"player"}, Reasons: []string{"left"}}, ActivityCodecs[NoFields, int, NoFields]{Participant: codec})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := kind.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	return activity, tel
}
func participantRef(tel *Telemetry, index byte, consented bool) SessionRef {
	ref := SessionRef{owner: tel, id: strings.Repeat("0", 31) + "1", visit: strings.Repeat("0", 31) + "2", client: schema.Client{Platform: "linux", Browser: "firefox", Device: "desktop"}, permitted: consented}
	ref.token[0] = index
	return ref
}
func TestParticipantSeatsAndCopiedFields(t *testing.T) {
	a, tel := participantFixture(t)
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true, Fields: 1})
	if err != nil || len(p.ID()) != 32 {
		t.Fatal(err)
	}
	if _, err = a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Fields: 2}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for _, seat := range []int{-2, 32} {
		if _, err = a.Participant(ParticipantStart[int]{Seat: seat, Role: "player"}); err == nil {
			t.Fatal(seat)
		}
	}
	if _, err = a.Participant(ParticipantStart[int]{Seat: -1, Role: "player", Fields: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Participant(ParticipantStart[int]{Seat: 1, Role: "private-role"}); err == nil || strings.Contains(err.Error(), "private-role") {
		t.Fatal(err)
	}
	if err = p.Set(3); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := a.Snapshot()
	snapshot.Participants[0].Fields[0].Int = 900
	next, _ := a.Snapshot()
	if next.Participants[0].Fields[0].Int != 3 {
		t.Fatal("participant view retained mutable fields")
	}
	r, err := a.End(ActivityEnd[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	r.Wait(context.Background())
	if err = p.Set(4); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestParticipantDuplicateSupersededAndStaleRefs(t *testing.T) {
	a, tel := participantFixture(t)
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	one, two := participantRef(tel, 1, false), participantRef(tel, 2, false)
	if err = p.Joined(one); err != nil {
		t.Fatal(err)
	}
	if err = p.Joined(one); err != nil {
		t.Fatal(err)
	}
	if err = p.Joined(two); err != nil {
		t.Fatal(err)
	}
	if err = p.Left(one, "left"); err != nil {
		t.Fatal(err)
	}
	if err = p.Left(two, "left"); err != nil {
		t.Fatal(err)
	}
	if err = p.Left(two, "left"); err != nil {
		t.Fatal(err)
	}
	v, _ := a.Snapshot()
	seat := v.Participants[0]
	if seat.Joins != 2 || seat.Leaves != 1 || seat.Reconnects != 1 || len(seat.Reasons) != 1 || seat.Reasons[0].Count != 1 {
		t.Fatal(seat)
	}
	if seat.Client != nil || len(seat.Sessions) != 0 {
		t.Fatal("unconsented ref persisted client/session data")
	}
	foreign := one
	foreign.owner = &Telemetry{}
	if err = p.Joined(foreign); err == nil {
		t.Fatal("foreign ref admitted")
	}
	if err = p.Joined(SessionRef{}); err == nil {
		t.Fatal("zero ref admitted")
	}
}
func TestParticipantConsentLinksAndHistoryBound(t *testing.T) {
	a, tel := participantFixture(t)
	tel.opts.Sessions.Enabled = true
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := byte(1); i <= 20; i++ {
		ref := participantRef(tel, i, true)
		ref.id = fmt.Sprintf("%032x", i)
		if err = p.Joined(ref); err != nil {
			t.Fatal(i, err)
		}
	}
	v, _ := a.Snapshot()
	seat := v.Participants[0]
	if seat.Joins != 20 || seat.Reconnects != 19 || len(seat.Sessions) > 16 || !seat.LinksTruncated || seat.Client == nil {
		t.Fatal(seat)
	}
	v.Participants[0].Sessions[0].ID = "private-canary"
	after, _ := a.Snapshot()
	if after.Participants[0].Sessions[0].ID == "private-canary" {
		t.Fatal("mutable link copy")
	}
	bot, err := a.Participant(ParticipantStart[int]{Seat: 1, Role: "player", Human: false})
	if err != nil {
		t.Fatal(err)
	}
	if err = bot.Joined(participantRef(tel, 33, true)); err != nil {
		t.Fatal(err)
	}
	v, _ = a.Snapshot()
	if v.Participants[1].Client != nil || len(v.Participants[1].Sessions) != 0 {
		t.Fatal("bot persisted a client association")
	}
}
func TestParticipantSeatPresenceUsesElapsedTime(t *testing.T) {
	a, tel := participantFixture(t)
	clock := tel.opts.Clock.(interface {
		Advance(time.Duration) error
		JumpWall(time.Duration)
	})
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	ref := participantRef(tel, 1, false)
	p.Joined(ref)
	clock.Advance(3 * time.Second)
	clock.JumpWall(-time.Hour)
	p.Left(ref, "left")
	v, _ := a.Snapshot()
	if v.Participants[0].SeatPresenceMS != 3000 {
		t.Fatal(v.Participants[0])
	}
}

func TestParticipantLivePresenceDoesNotDoubleCountProjection(t *testing.T) {
	a, tel := participantFixture(t)
	clock := tel.opts.Clock.(interface{ Advance(time.Duration) error })
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	one, two := participantRef(tel, 1, false), participantRef(tel, 2, false)
	if err = p.Joined(one); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	for i := 0; i < 2; i++ {
		v, err := a.Snapshot()
		if err != nil || v.Participants[0].SeatPresenceMS != 1000 {
			t.Fatal(v, err)
		}
	}
	if err = p.Set(1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if err = p.Joined(two); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if err = p.Left(one, "left"); err != nil {
		t.Fatal(err)
	}
	receipt, err := a.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err = receipt.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	final, err := a.End(ActivityEnd[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	if err = final.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	v, err := a.Snapshot()
	if err != nil || v.Participants[0].SeatPresenceMS != 4000 {
		t.Fatal(v, err)
	}
}

func TestParticipantLinksYieldToReservedFinalCapacity(t *testing.T) {
	a, tel := participantFixture(t)
	tel.opts.Sessions.Enabled = true
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := a.Snapshot()
	// Find the smallest configured cap that already fits this admitted seat.
	cap := 0
	for limit := 1000; limit < 16<<10; limit++ {
		tel.opts.Activities.MaxRecordBytes = limit
		if err := a.entity.kind.validateFinalCapacity(original, false); err == nil {
			cap = limit
			break
		}
	}
	if cap == 0 {
		t.Fatal("seat does not fit any supported cap")
	}
	for i := byte(1); i <= 20; i++ {
		ref := participantRef(tel, i, true)
		ref.id = fmt.Sprintf("%032x", i)
		if err = p.Joined(ref); err != nil {
			t.Fatal(i, err)
		}
	}
	v, _ := a.Snapshot()
	seat := v.Participants[0]
	if seat.Joins != 20 || seat.Reconnects != 19 || len(seat.Sessions) != 0 || !seat.LinksTruncated {
		t.Fatal(seat)
	}
	receipt, err := a.End(ActivityEnd[NoFields]{})
	if err != nil {
		t.Fatal("links made final impossible", err)
	}
	tel.collectActivityReceipts()
	if err = receipt.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestParticipantConcurrentJoinsCountOnlyCommittedTransitions(t *testing.T) {
	a, tel := participantFixture(t)
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		t.Fatal(err)
	}
	var committed atomic.Uint64
	var group sync.WaitGroup
	for i := byte(1); i <= 32; i++ {
		group.Add(1)
		go func(index byte) {
			defer group.Done()
			err := p.Joined(participantRef(tel, index, false))
			if err == nil {
				committed.Add(1)
			} else if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrCapacity) {
				t.Errorf("join: %v", err)
			}
		}(i)
	}
	group.Wait()
	v, err := a.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if v.Participants[0].Joins != committed.Load() || v.Participants[0].Reconnects != committed.Load()-1 {
		t.Fatal(v.Participants[0], committed.Load())
	}
	if e := a.entity; e.presence[p.id].ref.id != "" || e.presence[p.id].ref.client != (schema.Client{}) {
		t.Fatal("active ref retained correlation payload")
	}
}

func BenchmarkParticipantDuplicateJoin(b *testing.B) {
	a, tel := participantFixture(b)
	p, err := a.Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true})
	if err != nil {
		b.Fatal(err)
	}
	ref := participantRef(tel, 1, false)
	if err = p.Joined(ref); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err = p.Joined(ref); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSessionRefExposesOnlyPermittedFrameworkID(t *testing.T) {
	_, tel := participantFixture(t)
	for _, ref := range []SessionRef{{}, participantRef(tel, 1, false), participantRef(tel, 2, true)} {
		if ref.Valid() || ref.ID() != "" {
			t.Fatal("disabled/unconsented source exposed a correlation ID")
		}
	}
	tel.opts.Sessions.Enabled = true
	ref := participantRef(tel, 3, true)
	if !ref.Valid() || ref.ID() != ref.id {
		t.Fatal("permitted reference missing")
	}
}
