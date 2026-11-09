self.first = { probe: !!self.__drive, patched: String(setTimeout).indexOf("[native code]") < 0, perf: performance.now(), date: Date.now(), origin: performance.timeOrigin };
postMessage({ kind: "first", first: self.first });
setTimeout(function () { postMessage({ kind: "t100", perf: performance.now(), date: Date.now() }); }, 100);
setTimeout(function () { postMessage({ kind: "t250", perf: performance.now(), date: Date.now() }); }, 250);
var n = 0;
var iv = setInterval(function () { n++; postMessage({ kind: "iv", n: n, perf: performance.now() }); if (n === 5) clearInterval(iv); }, 50);
onmessage = function (e) {
  if (e.data === "now") postMessage({ kind: "now", perf: performance.now(), date: Date.now(), origin: performance.timeOrigin, v: self.__drive ? __drive.now() : null });
};
