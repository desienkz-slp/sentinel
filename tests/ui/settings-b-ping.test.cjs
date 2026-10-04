const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const html = fs.readFileSync(path.join(__dirname, '../../web/settings.html'), 'utf8');
const source = html.slice(html.indexOf('async function pingEndpointB()'), html.indexOf('// ---------- Direktori Staf'));

for (const key of ['', 'typed-b-key']) {
  test(`B ping uses saved fields and preserves ${key ? 'explicit' : 'omitted'} key`, async () => {
    const fields = {sCodexBase: '', sCodexKey: key, sCodex: '', sCodexWire: 'responses'};
    const calls = [];
    const messages = [];
    const context = vm.createContext({
      $: id => ({value: fields[id]}),
      validateURL: (id, required) => {assert.equal(required, undefined); return fields[id];},
      msg: (...args) => messages.push(args),
      api: async (url, opts) => {calls.push({url, body: JSON.parse(opts.body)}); return {ok:true, model:'saved-b-model', latency_ms:1};},
    });
    vm.runInContext(source, context);
    await context.pingEndpointB();
    assert.equal(calls.length, 1, JSON.stringify(messages));
    assert.equal(calls[0].url, '/api/llm/ping');
    assert.deepEqual(calls[0].body, {endpoint:'b', wire_api:'responses', ...(key ? {api_key:key} : {})});
    assert.equal(messages.at(-1)[1], true);
  });
}
