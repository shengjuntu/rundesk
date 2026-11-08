// Keep local test evidence outside the maintained source and documentation tree.
const fs = require('node:fs');
const path = require('node:path');

module.exports = function qaOutputFor(script) {
  const name = path.basename(script, path.extname(script));
  const directory = path.resolve(__dirname, '..', 'build', 'qa', name);
  return function qaOutput(relative = '') {
    const output = path.join(directory, relative);
    fs.mkdirSync(path.dirname(output), {recursive: true});
    return output;
  };
};
