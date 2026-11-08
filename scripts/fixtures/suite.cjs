const fs = require('node:fs');
const path = require('node:path');
const qaOutputFor = require('../qa-output.cjs');

// Sequential named cases retain independent VM contexts and report identities.
async function runSuite(script, cases) {
  const output = qaOutputFor(script);
  const reports = [];
  const saveSummary = passed => {
    const summary = {passed, scope: 'Named test cases; each case states its actual execution boundary', cases: reports};
    const summaryPath = process.env.UI_REPORT_PATH || output('summary.json');
    fs.mkdirSync(path.dirname(summaryPath), {recursive: true});
    fs.writeFileSync(summaryPath, JSON.stringify(summary, null, 2) + '\n');
    return summary;
  };
  for (const {name, run, report} of cases) {
    let result;
    try { result = await run(); }
    catch (cause) {
      reports.push({name, passed: false, error: cause.message});
      saveSummary(false);
      throw new Error(`${name}: ${cause.message}`, {cause});
    }
    if (report) fs.writeFileSync(output(report), JSON.stringify(result, null, 2) + '\n');
    reports.push({name, ...result});
  }
  const summary = saveSummary(true);
  console.log(JSON.stringify(summary));
  return summary;
}
module.exports = {runSuite};
