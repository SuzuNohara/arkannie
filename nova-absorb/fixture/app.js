// multi-lens fixture for the audit.ann fan-out smoke
const API_KEY = "sk-live-1234567890abcdefghij"; // security: hardcoded secret

function getUsers(ids) {
  // perf: N+1 query in a loop; security: string-concatenated SQL
  return ids.map(id => db.query("SELECT * FROM users WHERE id=" + id));
}

function process(d) {
  // health: deep nesting + unclear names + implicit undefined return
  var x;
  if (d) { if (d.a) { if (d.a.b) { return d.a.b.c; } } }
  return x;
}

module.exports = { getUsers, process, API_KEY };
