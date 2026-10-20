package aot

import (
	"encoding/base64"
	"reflect"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestLinkedBindingThrowRejectsCheckpointAndCommitBeforeAbort(t *testing.T) {
	l, _, module, document := eventTransactionFixture(t)
	data := struct {
		Document                   string
		FrameTable, FrameSequences uint32
	}{base64.StdEncoding.EncodeToString(document), l.frameTable, l.frameSequences}
	var got struct {
		Statuses         []int32
		Threw, Preserved bool
		Bindings         int
	}
	runExpressionModule(t, module, `
  const api = instance.exports, statuses = [];
  let bindings = 0, threw = false;
  const committed = Buffer.from(memory.slice(api.committed(),api.committed()+65536));
  const frames = Buffer.from(memory.slice(data.FrameTable,data.FrameTable+256));
  const sequences = Buffer.from(memory.slice(data.FrameSequences,data.FrameSequences+128));
  const document = Buffer.from(data.Document,'base64'); memory.set(document,32768);
  try { api.initPage(32768,document.length); } catch { threw = true; }
  statuses.push(api.checkpoint(1,32768,32768),api.commit(0,0),api.abortPage());
  const preserved = committed.equals(Buffer.from(memory.slice(api.committed(),api.committed()+65536)))
    &&frames.equals(Buffer.from(memory.slice(data.FrameTable,data.FrameTable+256)))
    &&sequences.equals(Buffer.from(memory.slice(data.FrameSequences,data.FrameSequences+128)))
    &&api.pending()===0&&api.status()===0;
  statuses.push(api.checkpoint(0,32768,32768),api.initPage(32768,document.length),api.abortPage());
  process.stdout.write(JSON.stringify({Statuses:statuses,Threw:threw,Preserved:preserved,Bindings:bindings}));`, data, &got,
		`{input:unexpected,bind:()=>{if (++bindings===2) throw new Error('binding failed'); return 0;},patch:unexpected}`)
	if !got.Threw || !got.Preserved || got.Bindings != 2 || !reflect.DeepEqual(got.Statuses, []int32{-6, 6, 0, -8, 8, 0}) {
		t.Fatalf("binding exception allowed publication or survived abort: %+v", got)
	}
}

func TestEmitInputThrowRejectsCommitAndAbortAllowsNextEvent(t *testing.T) {
	u, leaf := scalarInputUnit(t, "event", Int, false)
	e := inputTestModule(t, u, leaf)
	data := struct {
		Record string
		Throw  bool
	}{scalarTransport(program.TypeInt, 0, 7, ""), true}
	var got struct {
		Statuses         []int32
		Threw, Preserved bool
	}
	runExpressionModule(t, e.module, `
  const api = instance.exports, statuses = [];
  statuses.push(api.begin(0,0,1),api.commit(0,0));
  const generation = api.committed(), committed = Buffer.from(memory.slice(generation,generation+65536));
  statuses.push(api.begin(1,0,0));
  let threw = false;
  try { api.read(0); } catch { threw = true; }
  statuses.push(api.status(),api.commit(1,0),api.abort(),api.status());
  const preserved = generation===api.committed()&&committed.equals(Buffer.from(memory.slice(generation,generation+65536)));
  data.Throw = false;
  statuses.push(api.begin(2,0,0));
  const pointer = api.read(0);
  const record = Buffer.from(data.Record,'base64');
  if (!pointer||!Buffer.from(memory.slice(pointer,pointer+16)).equals(record.subarray(0,16))) throw new Error('input recovery failed');
  statuses.push(api.status(),api.commit(2,0));
  process.stdout.write(JSON.stringify({Statuses:statuses,Threw:threw,Preserved:preserved}));`, data, &got,
		`{input:(id,field,dst,cap)=>{const record=Buffer.from(data.Record,'base64'); memory.set(record,dst); if(data.Throw) throw new Error('input failed'); return record.length;},bind:unexpected,patch:unexpected}`)
	if !got.Threw || !got.Preserved || !reflect.DeepEqual(got.Statuses, []int32{0, 0, 0, 2, 2, 0, 0, 0, 0, 0}) {
		t.Fatalf("input exception allowed publication or blocked recovery: %+v", got)
	}
}
