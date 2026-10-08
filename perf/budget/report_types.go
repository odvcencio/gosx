package budget

// Public record types contain only the versioned allowlist. Native execution
// options, URLs, raw traces and arbitrary metadata never enter these structs.

type PublicInfo struct {
	SHA                   string  `json:"sha"`
	BaseSHA               string  `json:"baseSHA,omitempty"`
	EpochSHA256           *string `json:"epochSHA256"`
	ProfileSHA256         string  `json:"profileSHA256"`
	CoefficientSHA256     string  `json:"coefficientSHA256"`
	ToolchainSHA256       string  `json:"toolchainSHA256"`
	FixtureSHA256         string  `json:"fixtureSHA256"`
	Runner                string  `json:"runner"`
	Canonical             bool    `json:"canonical"`
	Transport             string  `json:"transport"`
	Headless              bool    `json:"headless"`
	Muted                 bool    `json:"muted"`
	BenchmarkIndexRounded *int64  `json:"benchmarkIndexRounded,omitempty"`
	CPUMultiplierMilli    *int64  `json:"cpuMultiplierMilli,omitempty"`
	Backend               string  `json:"backend,omitempty"`
	RendererClass         string  `json:"rendererClass,omitempty"`
	ChromeProduct         string  `json:"chromeProduct,omitempty"`
	ChromeSnapshot        string  `json:"chromeSnapshot,omitempty"`
	ArtifactSHA256        *string `json:"artifactSHA256"`
	BaseArtifactSHA256    string  `json:"baseArtifactSHA256,omitempty"`
}

type Cell struct {
	App           string `json:"app"`
	RouteTemplate string `json:"routeTemplate"`
	PageType      string `json:"pageType"`
	Scenario      string `json:"scenario"`
	Metric        string `json:"metric"`
	Backend       string `json:"backend"`
	Unit          string `json:"unit"`
}

type Row struct {
	App                   string         `json:"app"`
	RouteTemplate         string         `json:"routeTemplate"`
	PageType              string         `json:"pageType"`
	Scenario              string         `json:"scenario"`
	NormalizedBytes       int64          `json:"normalizedBytes"`
	WireBytes             int64          `json:"wireBytes"`
	FrameworkBytes        int64          `json:"frameworkBytes"`
	AppBytes              int64          `json:"appBytes"`
	BaseBytes             *int64         `json:"baseBytes"`
	DeltaBytes            *int64         `json:"deltaBytes"`
	AllocationBytes       int64          `json:"allocationBytes"`
	HeadroomBytes         int64          `json:"headroomBytes"`
	Requests              int64          `json:"requests"`
	Policies              []PolicyResult `json:"policies"`
	ReasonCode            string         `json:"reasonCode"`
	Status                string         `json:"status"`
	Backend               string         `json:"backend"`
	PhaseBytes            PhaseBytes     `json:"phaseBytes"`
	FrameworkCeilingBytes int64          `json:"frameworkCeilingBytes"`
	AppRemainingBytes     int64          `json:"appRemainingBytes"`
	ModelMicros           *int64         `json:"modelMicros"`
	ModelStatus           string         `json:"modelStatus"`
}

type Report struct {
	Schema          string        `json:"schema"`
	Info            PublicInfo    `json:"info"`
	Mode            string        `json:"mode"`
	Rows            []Row         `json:"rows"`
	Assets          []AssetReport `json:"assets"`
	ExceptionIDs    []string      `json:"exceptionIDs"`
	Passed          bool          `json:"passed"`
	Acknowledgments []Ack         `json:"acknowledgments"`
	Violations      []CountReason `json:"violations"`
	Coverage        ByteCoverage  `json:"coverage"`
}

type SeriesPoint struct {
	Schema         string        `json:"schema"`
	Info           PublicInfo    `json:"info"`
	Cell           Cell          `json:"cell"`
	At             string        `json:"at"`
	RunOrdinal     int64         `json:"runOrdinal"`
	N              int64         `json:"n"`
	Samples        []float64     `json:"samples"`
	Median         float64       `json:"median"`
	P75            float64       `json:"p75"`
	MAD            float64       `json:"mad"`
	Partial        bool          `json:"partial"`
	InvalidReasons []CountReason `json:"invalidReasons"`
}

type Interval struct {
	Lower         *float64 `json:"lower"`
	Upper         *float64 `json:"upper"`
	ConfidencePPM int64    `json:"confidencePPM"`
	Bounded       bool     `json:"bounded"`
}

type PairReport struct {
	Schema     string     `json:"schema"`
	Info       PublicInfo `json:"info"`
	FamilySize int64      `json:"familySize"`
	Looks      []int64    `json:"looks"`
	Seed       uint64     `json:"seed"`
	Test       string     `json:"test"`
	Cells      []PairCell `json:"cells"`
	PlanSHA256 string     `json:"planSHA256"`
	Status     string     `json:"status"`
}

type FieldSnapshot struct {
	Schema      string       `json:"schema"`
	WindowStart string       `json:"windowStart"`
	WindowEnd   string       `json:"windowEnd"`
	Vitals      []FieldVital `json:"vitals"`
	Hubs        []FieldHub   `json:"hubs"`
}

type RunStatus struct {
	Schema         string     `json:"schema"`
	Info           PublicInfo `json:"info"`
	Status         string     `json:"status"`
	ReasonCode     string     `json:"reasonCode"`
	PlannedCells   int64      `json:"plannedCells"`
	CompletedCells int64      `json:"completedCells"`
}

type PhaseBytes struct {
	Critical   int64 `json:"critical"`
	Startup    int64 `json:"startup"`
	AfterReady int64 `json:"afterReady"`
	Dormant    int64 `json:"dormant"`
}

type SizeTriple struct {
	Raw    int64 `json:"raw"`
	Gzip   int64 `json:"gzip"`
	Brotli int64 `json:"brotli"`
}

type CountReason struct {
	ReasonCode string `json:"reasonCode"`
	Count      int64  `json:"count"`
}

type Ack struct {
	Kind        string  `json:"kind"`
	Scope       string  `json:"scope"`
	Metric      string  `json:"metric"`
	Delta       int64   `json:"delta"`
	Issue       int64   `json:"issue"`
	Disposition string  `json:"disposition"`
	ReasonCode  string  `json:"reasonCode"`
	Expires     *string `json:"expires"`
}

type ByteCoverage struct {
	RoutesExpected int64  `json:"routesExpected"`
	RoutesMeasured int64  `json:"routesMeasured"`
	AssetsExpected int64  `json:"assetsExpected"`
	AssetsMeasured int64  `json:"assetsMeasured"`
	Reachability   string `json:"reachability"`
}

type NumericHistogram struct {
	Unit             string    `json:"unit"`
	Bounds           []float64 `json:"bounds"`
	CumulativeCounts []int64   `json:"cumulativeCounts"`
	Count            int64     `json:"count"`
}

type PolicyResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type AssetReport struct {
	ID             string      `json:"id"`
	SHA256         string      `json:"sha256"`
	Owner          string      `json:"owner"`
	Phase          string      `json:"phase"`
	Raw            int64       `json:"raw"`
	Gzip           int64       `json:"gzip"`
	Brotli         int64       `json:"brotli"`
	ChangedSources []string    `json:"changedSources"`
	App            string      `json:"app"`
	Kind           string      `json:"kind"`
	Condition      string      `json:"condition"`
	Dependencies   []string    `json:"dependencies"`
	BaseSizes      *SizeTriple `json:"baseSizes"`
}

type PairCell struct {
	Cell                Cell          `json:"cell"`
	BaseSamples         []float64     `json:"baseSamples"`
	HeadSamples         []float64     `json:"headSamples"`
	Pairs               int64         `json:"pairs"`
	InvalidPairs        int64         `json:"invalidPairs"`
	HL                  *float64      `json:"hl"`
	MedianDifference    *float64      `json:"medianDifference"`
	AdjustedInterval    Interval      `json:"adjustedInterval"`
	DescriptiveInterval Interval      `json:"descriptiveInterval"`
	PNumerator          *string       `json:"pNumerator"`
	PDenominator        *string       `json:"pDenominator"`
	RankBiserial        *float64      `json:"rankBiserial"`
	Threshold           float64       `json:"threshold"`
	Decision            string        `json:"decision"`
	InvalidReasons      []CountReason `json:"invalidReasons"`
	StoppingLook        int64         `json:"stoppingLook"`
}

type FieldVital struct {
	App              string    `json:"app"`
	RouteTemplate    string    `json:"routeTemplate"`
	Metric           string    `json:"metric"`
	Device           string    `json:"device"`
	Count            int64     `json:"count"`
	Bounds           []float64 `json:"bounds"`
	CumulativeCounts []int64   `json:"cumulativeCounts"`
	ReasonCode       string    `json:"reasonCode"`
}

type FieldHub struct {
	App              string            `json:"app"`
	Hub              string            `json:"hub"`
	Direction        string            `json:"direction"`
	Kind             string            `json:"kind"`
	Messages         int64             `json:"messages"`
	PayloadBytes     int64             `json:"payloadBytes"`
	ClientSeconds    float64           `json:"clientSeconds"`
	ReferenceClients int64             `json:"referenceClients"`
	MaxPayloadBytes  *int64            `json:"maxPayloadBytes"`
	ReasonCode       string            `json:"reasonCode"`
	QueueDepth       *NumericHistogram `json:"queueDepth"`
	RTT              *NumericHistogram `json:"rtt"`
	Drops            *int64            `json:"drops"`
	RateLimited      *int64            `json:"rateLimited"`
	SlowEvictions    *int64            `json:"slowEvictions"`
}
