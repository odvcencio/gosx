'use strict';

function countDiagnostic(content, needle) {
  let count = 0;
  let offset = 0;
  for (;;) {
    const found = content.indexOf(needle, offset);
    if (found < 0) return count;
    count += 1;
    offset = found + Math.max(1, needle.length);
  }
}

function normalizeOwnedRanges(rawLength, ranges) {
  if (!Number.isInteger(rawLength) || rawLength < 0) {
    throw new Error('Chrome stderr length must be a non-negative integer');
  }
  if (!Array.isArray(ranges) || ranges.length === 0) {
    throw new Error('live renderer stderr ranges are missing');
  }
  const names = new Set();
  let previousEnd = 0;
  return ranges.map((entry, index) => {
    const name = entry && typeof entry.name === 'string' ? entry.name : '';
    const startByte = entry && entry.startByte;
    const beforeTargetCloseByte = entry && entry.beforeTargetCloseByte;
    if (!name || names.has(name)) {
      throw new Error('live renderer stderr range ' + index + ' has an invalid name');
    }
    if (!Number.isInteger(startByte) || !Number.isInteger(beforeTargetCloseByte) ||
        startByte < 0 || beforeTargetCloseByte < startByte ||
        beforeTargetCloseByte > rawLength) {
      throw new Error('live renderer stderr range ' + name + ' has invalid bounds');
    }
    if (index > 0 && startByte < previousEnd) {
      throw new Error('live renderer stderr range ' + name + ' overlaps its predecessor');
    }
    names.add(name);
    previousEnd = beforeTargetCloseByte;
    return { name, startByte, beforeTargetCloseByte };
  });
}

function buildOwnedChromeStderrRanges(capabilityRange, caseRanges) {
  const startByte = capabilityRange && capabilityRange.startByte;
  const afterTargetCloseByte = capabilityRange && capabilityRange.afterTargetCloseByte;
  if (!Number.isInteger(startByte) || !Number.isInteger(afterTargetCloseByte) ||
      startByte < 0 || afterTargetCloseByte < startByte) {
    throw new Error('capability stderr range has invalid bounds');
  }
  if (!Array.isArray(caseRanges) || caseRanges.length === 0) {
    throw new Error('live renderer stderr ranges are missing');
  }
  if (!caseRanges[0] || caseRanges[0].startByte < afterTargetCloseByte) {
    throw new Error('first live renderer stderr range overlaps capability teardown');
  }
  return [
    { name: 'chrome-startup', startByte: 0, beforeTargetCloseByte: startByte },
    ...caseRanges,
  ];
}

function diagnosticFindings(raw, ranges, needles) {
  return needles.map((needle) => {
    let count = 0;
    for (const range of ranges) {
      const content = raw.subarray(range.startByte, range.beforeTargetCloseByte)
        .toString('utf8').toLowerCase();
      count += countDiagnostic(content, needle);
    }
    return { needle, count };
  }).filter((entry) => entry.count > 0);
}

function scanOwnedChromeDiagnostics(raw, ranges, swapDiagnostics, lifecycleDiagnostics) {
  if (!Buffer.isBuffer(raw)) throw new Error('Chrome stderr must be a Buffer');
  const ownedStderrRanges = normalizeOwnedRanges(raw.length, ranges);
  const swapNeedles = Array.isArray(swapDiagnostics) ? swapDiagnostics : [];
  const lifecycleNeedles = Array.isArray(lifecycleDiagnostics) ? lifecycleDiagnostics : [];
  return {
    ownedStderrBytes: ownedStderrRanges.reduce((total, range) =>
      total + range.beforeTargetCloseByte - range.startByte, 0),
    ownedStderrRanges,
    swapFindings: diagnosticFindings(raw, ownedStderrRanges, swapNeedles),
    preTeardownLifecycleFindings:
      diagnosticFindings(raw, ownedStderrRanges, lifecycleNeedles),
  };
}

function chromeDiagnosticFailures(diagnostics) {
  if (!diagnostics || typeof diagnostics !== 'object') {
    return ['Chrome stderr diagnostic scan failed: missing diagnostics'];
  }
  const failures = [];
  if (diagnostics.scanError) {
    failures.push('Chrome stderr diagnostic scan failed: ' + diagnostics.scanError);
  }
  if (Array.isArray(diagnostics.swapFindings) && diagnostics.swapFindings.length > 0) {
    failures.push('Chrome stderr contains forbidden swap/SharedImage diagnostics: ' +
      JSON.stringify(diagnostics.swapFindings));
  }
  if (Array.isArray(diagnostics.preTeardownLifecycleFindings) &&
      diagnostics.preTeardownLifecycleFindings.length > 0) {
    failures.push('Chrome stderr contains pre-teardown WebGPU lifecycle diagnostics: ' +
      JSON.stringify(diagnostics.preTeardownLifecycleFindings));
  }
  return failures;
}

module.exports = {
  buildOwnedChromeStderrRanges,
  chromeDiagnosticFailures,
  normalizeOwnedRanges,
  scanOwnedChromeDiagnostics,
};
