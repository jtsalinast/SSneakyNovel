// The Log panel mirrors the terminal, so consecutive identical requests
// (GET /api/status every two seconds) would bury everything else. Consecutive
// entries with the same request key collapse into one accordion group.
export function logGroupKey(msg) {
  const m = /(?:^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}\s+)?([A-Z]{3,7} \S+) \(/.exec(msg || '');
  return m ? m[1] : '';
}

export function groupLogEntries(entries) {
  const out = [];
  for (const entry of entries || []) {
    const key = logGroupKey(entry.msg);
    const last = out[out.length - 1];
    if (key && last && last.key === key) {
      last.entries.push(entry);
      last.time = entry.time;
    } else {
      out.push({ key, entries: [entry], time: entry.time });
    }
  }
  return out;
}
