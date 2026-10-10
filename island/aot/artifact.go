package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/wasmgen"
)

const (
	ABIVersion         = 1
	EncoderRevision    = 1
	HelperRevision     = 1
	ProgramRawLimit    = 16 * 1024
	ProgramBrotliLimit = 4 * 1024
	PageRawLimit       = 64 * 1024
	PageBrotliLimit    = 16 * 1024
)

// Options selects an exact compiler contract. Profile limits and revisions
// cannot be widened or substituted by callers.
type Options struct {
	Profile              Profile
	ABI, Encoder, Helper uint32
	Limits               Limits
}

func DefaultOptions() Options {
	return Options{ScalarDOMV1, ABIVersion, EncoderRevision, HelperRevision, ProfileLimits()}
}

// ProgramIdentity is assigned by byte-sorted unit digest, independently of
// component names or caller insertion order.
type ProgramIdentity struct {
	ID                              uint32
	Component                       string
	Digest, ProgramSHA, ContractSHA [32]byte
}

type SizeReceipt struct {
	SHA, BrotliSHA        [32]byte
	RawBytes, BrotliBytes uint32
}

type ProgramDescriptor struct {
	Identity ProgramIdentity
	Bindings BindingSet
	Contract ScalarContract
}

// Artifact owns canonical module and sidecar bytes and the identities they
// implement. Standalone receipts use the complete profile with one unit.
type Artifact struct {
	Bytes, Brotli []byte
	Receipt       SizeReceipt
	InputSetSHA   [32]byte
	Options       Options
	Programs      []ProgramDescriptor
	Standalone    []SizeReceipt
	Eligibility   []Eligibility
}

func optionBytes(o Options) ([]byte, error) {
	if o != DefaultOptions() {
		return nil, fmt.Errorf("compiler options do not match the profile")
	}
	fields := []uint32{o.ABI, uint32(o.Profile), o.Encoder, o.Helper,
		o.Limits.Nodes, o.Limits.Expressions, o.Limits.Handlers, o.Limits.Computeds,
		o.Limits.NodeDepth, o.Limits.ExpressionDepth, o.Limits.ComputedDepth,
		o.Limits.Signals, o.Limits.Inputs, o.Limits.Programs, o.Limits.Instances, o.Limits.SharedNames,
		o.Limits.Values, o.Limits.StringBytes, o.Limits.InputBytes, o.Limits.CommittedStringBytes,
		o.Limits.Patches, o.Limits.PendingEvents, o.Limits.QueueBytes, o.Limits.ConstantsBytes, o.Limits.MemoryBytes}
	out := make([]byte, 0, len(fields)*4)
	for _, field := range fields {
		out = binary.LittleEndian.AppendUint32(out, field)
	}
	return out, nil
}

func canonicalUnits(units []Unit, o Options) ([]Unit, []Eligibility, [32]byte, error) {
	options, err := optionBytes(o)
	if err != nil {
		return nil, nil, [32]byte{}, err
	}
	if len(units) == 0 {
		return nil, nil, [32]byte{}, fmt.Errorf("program set is empty")
	}
	unique := map[[32]byte]Unit{}
	components := map[string][32]byte{}
	for _, unit := range units {
		if receipt := Classify(unit, o.Profile); !receipt.Eligible {
			return nil, nil, [32]byte{}, fmt.Errorf("program profile: %s[%d]: %s", receipt.Table, receipt.Index, receipt.Reason)
		}
		if digest, exists := components[unit.Component]; exists && digest != unit.Digest {
			return nil, nil, [32]byte{}, fmt.Errorf("conflicting component identity")
		}
		components[unit.Component] = unit.Digest
		if _, exists := unique[unit.Digest]; !exists {
			owned, err := NewUnit(unit.Component, unit.Program, unit.Contract)
			if err != nil {
				return nil, nil, [32]byte{}, err
			}
			unique[unit.Digest] = owned
		}
	}
	if len(unique) > int(o.Limits.Programs) {
		return nil, nil, [32]byte{}, fmt.Errorf("program set exceeds the profile")
	}
	ordered := make([]Unit, 0, len(unique))
	for _, unit := range unique {
		ordered = append(ordered, unit)
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i].Digest[:], ordered[j].Digest[:]) < 0 })
	identity := append([]byte("gosx-aot-set-v1\x00"), options...)
	identity = binary.LittleEndian.AppendUint32(identity, uint32(len(ordered)))
	receipts := make([]Eligibility, 0, len(ordered))
	for _, unit := range ordered {
		identity = append(identity, unit.Digest[:]...)
		receipts = append(receipts, Classify(unit, o.Profile))
	}
	return ordered, receipts, sha256.Sum256(identity), nil
}

func moduleMetadata(units []Unit, o Options, digest [32]byte) []byte {
	var out []byte
	for _, field := range []uint32{o.ABI, o.Encoder, o.Helper, uint32(o.Profile)} {
		out = binary.LittleEndian.AppendUint32(out, field)
	}
	out = append(out, digest[:]...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(units)))
	for _, unit := range units {
		out = append(out, unit.ProgramSHA[:]...)
		out = append(out, unit.ContractSHA[:]...)
	}
	return out
}

func measureModule(raw []byte, standalone bool) ([]byte, SizeReceipt, error) {
	rawLimit, compressedLimit := uint32(PageRawLimit), uint32(PageBrotliLimit)
	if standalone {
		rawLimit, compressedLimit = ProgramRawLimit, ProgramBrotliLimit
	}
	if uint64(len(raw)) > uint64(rawLimit) {
		return nil, SizeReceipt{}, fmt.Errorf("module raw byte limit")
	}
	if err := wasmgen.Validate(raw); err != nil {
		return nil, SizeReceipt{}, fmt.Errorf("module validation: %w", err)
	}
	var compressed bytes.Buffer
	writer := brotli.NewWriterLevel(&compressed, 11)
	if _, err := writer.Write(raw); err != nil {
		return nil, SizeReceipt{}, err
	}
	if err := writer.Close(); err != nil {
		return nil, SizeReceipt{}, err
	}
	if uint64(compressed.Len()) > uint64(compressedLimit) {
		return nil, SizeReceipt{}, fmt.Errorf("module Brotli byte limit")
	}
	br := compressed.Bytes()
	return br, SizeReceipt{sha256.Sum256(raw), sha256.Sum256(br), uint32(len(raw)), uint32(len(br))}, nil
}

func materializeArtifact(module wasmgen.Module, units []Unit, o Options, standalone bool) (Artifact, error) {
	ordered, receipts, digest, err := canonicalUnits(units, o)
	if err != nil {
		return Artifact{}, err
	}
	if standalone && len(ordered) != 1 {
		return Artifact{}, fmt.Errorf("standalone proof requires one program")
	}
	module.Metadata = moduleMetadata(ordered, o, digest)
	raw, err := wasmgen.Encode(module)
	if err != nil {
		return Artifact{}, err
	}
	br, measurement, err := measureModule(raw, standalone)
	if err != nil {
		return Artifact{}, err
	}
	artifact := Artifact{Bytes: raw, Brotli: br, Receipt: measurement, InputSetSHA: digest, Options: o,
		Programs: []ProgramDescriptor{}, Standalone: []SizeReceipt{}, Eligibility: receipts}
	for i, unit := range ordered {
		bindings, err := BuildBindings(unit)
		if err != nil {
			return Artifact{}, err
		}
		artifact.Programs = append(artifact.Programs, ProgramDescriptor{ProgramIdentity{uint32(i), unit.Component, unit.Digest, unit.ProgramSHA, unit.ContractSHA}, bindings, unit.Contract})
	}
	if standalone {
		artifact.Standalone = append(artifact.Standalone, measurement)
	}
	return artifact, nil
}
