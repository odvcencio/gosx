package teamai

import "m31labs.dev/gosx/det"

// Contest is a one-on-one contest between an attacker and a defender: a
// block, a tackle, a steal, a duel for a loose object. The host decides what
// the two sides and their ratings mean.
type Contest struct {
	// Attacker and Defender are ratings on any scale; only their difference
	// counts, divided by ContestModel.Scale.
	Attacker float64
	Defender float64
	// Angle is the attacker's approach angle in radians from the ideal one,
	// 0 to Pi (see ApproachAngle).
	Angle float64
	// RelSpeed is the attacker's speed advantage into the contest, in units
	// per second (negative when the defender is faster).
	RelSpeed float64
	// Timing is how far the attacker is from the ideal moment, in seconds.
	// The sign is ignored.
	Timing float64
}

// Result is the outcome of a contest for the attacker.
type Result int8

const (
	// Loss: the defender wins.
	Loss Result = -1
	// Draw: neither side wins (a stalemate the host can resolve later).
	Draw Result = 0
	// Win: the attacker wins.
	Win Result = 1
)

func (r Result) String() string {
	switch r {
	case Win:
		return "win"
	case Loss:
		return "loss"
	}
	return "draw"
}

// ContestModel turns a Contest into a win chance with a logistic curve:
//
//	logit = Bias + (Attacker-Defender)/Scale
//	      + AngleWeight * cos(Angle)
//	      + SpeedWeight * clamp(RelSpeed, -SpeedCap, SpeedCap)
//	      + TimingWeight * (1 - 2*min(|Timing|/TimingWindow, 1))
//	p     = 1 / (1 + exp(-logit)), then kept within [Floor, 1-Floor]
//
// A perfect approach adds AngleWeight and the worst subtracts it; perfect
// timing adds TimingWeight and timing off by TimingWindow or more subtracts
// it. Draw is the width of the probability band around p that resolves as a
// draw. The zero model uses the defaults listed on each field.
type ContestModel struct {
	// Scale is the rating difference worth one logit unit. Zero means 10.
	Scale float64
	// Bias favours the attacker (positive) or the defender (negative).
	Bias float64
	// AngleWeight is the logit of a perfect approach. Zero means 1.
	AngleWeight float64
	// SpeedWeight is the logit per unit per second of speed advantage. Zero
	// means 0.15.
	SpeedWeight float64
	// SpeedCap bounds RelSpeed. Zero means 10.
	SpeedCap float64
	// TimingWeight is the logit of perfect timing. Zero means 0.5.
	TimingWeight float64
	// TimingWindow is the timing error, in seconds, that costs the whole
	// TimingWeight. Zero means 0.25.
	TimingWindow float64
	// Draw is the probability band that resolves as a draw, 0 to 1.
	Draw float64
	// Floor is the least chance either side keeps, 0 to 0.5.
	Floor float64
}

func (m *ContestModel) defaults() ContestModel {
	d := *m
	if d.Scale == 0 {
		d.Scale = 10
	}
	if d.AngleWeight == 0 {
		d.AngleWeight = 1
	}
	if d.SpeedWeight == 0 {
		d.SpeedWeight = 0.15
	}
	if d.SpeedCap == 0 {
		d.SpeedCap = 10
	}
	if d.TimingWeight == 0 {
		d.TimingWeight = 0.5
	}
	if d.TimingWindow == 0 {
		d.TimingWindow = 0.25
	}
	d.Draw = clamp(d.Draw, 0, 1)
	d.Floor = clamp(d.Floor, 0, 0.5)
	return d
}

// Logit returns the contest's log-odds for the attacker.
func (m *ContestModel) Logit(c Contest) float64 {
	d := m.defaults()
	x := d.Bias + (c.Attacker-c.Defender)/d.Scale
	x += float64(d.AngleWeight * det.Cos(clamp(c.Angle, 0, det.Pi)))
	x += float64(d.SpeedWeight * clamp(c.RelSpeed, -d.SpeedCap, d.SpeedCap))
	off := clamp(det.Abs(c.Timing)/d.TimingWindow, 0, 1)
	x += float64(d.TimingWeight * (1 - float64(2*off)))
	return x
}

// Prob returns the attacker's chance of winning, before the draw band.
func (m *ContestModel) Prob(c Contest) float64 {
	d := m.defaults()
	x := m.Logit(c)
	if !finite(x) {
		x = 0
	}
	p := 1 / (1 + det.Exp(-x))
	return clamp(p, d.Floor, 1-d.Floor)
}

// Chances returns the probabilities of a win, a draw and a loss. They sum to
// 1.
func (m *ContestModel) Chances(c Contest) (win, draw, loss float64) {
	d := m.defaults()
	p := m.Prob(c)
	half := float64(d.Draw * 0.5)
	win = det.Max(p-half, 0)
	loss = det.Max(1-p-half, 0)
	return win, 1 - win - loss, loss
}

// Outcome is a resolved contest.
type Outcome struct {
	// P is the attacker's chance of winning, from Prob.
	P float64
	// Roll is the seeded uniform draw in [0, 1).
	Roll float64
	// Result is Win when Roll falls below the win chance, Draw inside the
	// draw band, Loss above it.
	Result Result
	// Margin is P - Roll: how decisive the result was. Hosts can use it to
	// pick a stronger or weaker reaction.
	Margin float64
}

// Resolve settles the contest with a draw keyed by seed and three integers,
// typically the tick and the two agents: Resolve(c, w.Seed, w.Tick, a, b).
// The same contest and keys give the same Outcome on every platform, and
// over many keys the results follow Chances.
func (m *ContestModel) Resolve(c Contest, seed, k1, k2, k3 uint64) Outcome {
	win, draw, _ := m.Chances(c)
	p := m.Prob(c)
	u := Unit(Mix(seed, k1, k2, k3))
	o := Outcome{P: p, Roll: u, Margin: p - u}
	switch {
	case u < win:
		o.Result = Win
	case u < win+draw:
		o.Result = Draw
	default:
		o.Result = Loss
	}
	return o
}
