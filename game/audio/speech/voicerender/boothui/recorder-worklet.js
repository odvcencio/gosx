// Posts each 128-frame block of channel 0 to the page, with its peak, so the
// page can show a level meter and keep the samples for the take.
class BoothRecorder extends AudioWorkletProcessor {
  constructor() {
    super();
    this.on = false;
    this.port.onmessage = (e) => { this.on = !!e.data.record; };
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    let peak = 0;
    for (let i = 0; i < ch.length; i++) { const a = Math.abs(ch[i]); if (a > peak) peak = a; }
    if (this.on) {
      const copy = new Float32Array(ch);
      this.port.postMessage({ peak, samples: copy }, [copy.buffer]);
    } else {
      this.port.postMessage({ peak });
    }
    return true;
  }
}
registerProcessor("booth-recorder", BoothRecorder);
