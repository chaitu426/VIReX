import fs from 'node:fs';

const P = {
  caller: { id: 'caller', type: 'external', label: 'Caller', sublabel: 'AI agent · app' },
  api: { id: 'api', type: 'backend', label: 'Semantic Video API', sublabel: 'Go REST / gRPC' },
  index: { id: 'index', type: 'database', label: 'Index Reader', sublabel: 'time · entity' },
  sdec: { id: 'sdec', type: 'backend', label: 'Semantic Decoder', sublabel: 'blocks → SVIR' },
  pdec: { id: 'pdec', type: 'cloud', label: 'Pixel Decoder', sublabel: 'FFmpeg seek' },
  llm: { id: 'llm', type: 'frontend', label: 'LLM / VLM', sublabel: 'answers' },
};

const legend = { mode: 'auto', entries: {
  emphasis: { label: 'API request' }, return: { label: 'Response' },
  dashed: { label: 'Only when needed' }, default: { label: 'Internal call' } } };

function build({ slug, title, subtitle, height, cards, body }) {
  const messages = [], activations = [], segments = [];
  let y = 194;
  const STEP = 30;
  const msg = (from, to, label, variant = 'default', note) => {
    const m = { id: `m${messages.length + 1}`, from, to, y, label, variant, ...(note ? { note } : {}) };
    messages.push(m); y += STEP; return m;
  };
  const call = (to, callLabel, retLabel, opts = {}) => {
    const a = msg('api', to, callLabel, opts.variant ?? 'default', opts.note);
    const r = msg(to, 'api', retLabel, 'return');
    activations.push({ participant: to, from: a.y - 6, to: r.y + 6, type: P[to].type });
  };
  const segment = (label, fn) => {
    const start = y - 24, firstY = y;
    fn();
    const end = y - STEP + 18;
    segments.push({ from: start, to: end, label });
    activations.push({ participant: 'api', from: firstY - 6, to: end - 10, type: 'backend' });
    y += 26;
  };
  body({ msg, call, segment });
  const ids = new Set(['caller', 'api', ...messages.flatMap(m => [m.from, m.to])]);
  const order = Object.keys(P).filter(k => ids.has(k));
  const doc = {
    schema_version: 1, diagram_type: 'sequence',
    meta: { title, subtitle, output: `${slug}.html`, viewBox: [1000, height], column_fit: 'spread', quality_profile: 'showcase', legend },
    participants: order.map(k => P[k]), segments, messages, activations, cards,
  };
  fs.writeFileSync(`${slug}.json`, JSON.stringify(doc, null, 2));
  console.log(slug, 'last y', y);
}

build({
  slug: 'api-get-frame', height: 610,
  title: 'getFrame(t) — Request to Response',
  subtitle: 'Returns the decoded picture at time t together with everything the semantic stream knows about it.',
  body: ({ msg, call, segment }) => segment('GET /frame?t=10.5', () => {
    msg('caller', 'api', 'GET /frame?t=10.5', 'emphasis');
    call('index', 'find blocks at t', 'offsets, key frame');
    call('pdec', 'seek key frame, decode to t', 'frame pixels');
    call('sdec', 'decode blocks at t', 'objects · text · actions');
    msg('api', 'caller', 'frame + semantics', 'return');
  }),
  cards: [
    { dot: 'cyan', title: 'How it works', items: [
      'Temporal index maps t to semantic blocks and the nearest key frame',
      'Pixel decoder seeks to that key frame and decodes forward to t',
      'Semantic decoder reads only the blocks for t' ] },
    { dot: 'emerald', title: 'Used for', items: ['Showing a frame with its overlays', 'Visual verification by an agent'] },
  ],
});

build({
  slug: 'api-semantic-reads', height: 610,
  title: 'Semantic Reads — Request to Response',
  subtitle: 'getObjects, getText, getEvents, getTimeline and getEntity read semantic blocks only and never touch pixels.',
  body: ({ msg, call, segment }) => segment('GET /objects · /text · /events · /timeline · /entities/{id}', () => {
    msg('caller', 'api', 'GET semantic read', 'emphasis', 't · range · entity id');
    call('index', 'time range or entity', 'offsets');
    call('sdec', 'decode those blocks', 'objects · OCR · events');
    msg('api', 'caller', 'JSON, no pixels', 'return');
  }),
  cards: [
    { dot: 'cyan', title: 'Index used per call', items: [
      'getObjects(t), getText(t): temporal index',
      'getEvents(start, end), getTimeline(): temporal range scan',
      'getEntity(id): entity index (lifetime + blocks)' ] },
    { dot: 'emerald', title: 'Why it is fast', items: ['Index gives byte offsets', 'Only small blocks are decoded, not the full stream'] },
  ],
});

build({
  slug: 'api-search-semantic', height: 540,
  title: 'searchSemantic(query) — Request to Response',
  subtitle: 'Finds when something happens in the video using the text, event and vector indexes.',
  body: ({ msg, call, segment }) => segment('GET /search?q=teacher writing on board', () => {
    msg('caller', 'api', 'GET /search?q=...', 'emphasis');
    call('index', 'text · event · vector', 'ranked timestamps');
    msg('api', 'caller', 'time ranges + scores', 'return');
  }),
  cards: [
    { dot: 'cyan', title: 'How it works', items: [
      'Query terms hit the text and event inverted indexes',
      'Optional embedding lookup adds vector matches',
      'Results are merged and ranked by score' ] },
    { dot: 'emerald', title: 'Example result', items: ['04:12 - 04:31', '07:44 - 08:10', '12:02 - 12:26'] },
  ],
});

build({
  slug: 'api-query-video', height: 640,
  title: 'queryVideo(question) — Request to Response',
  subtitle: 'Answers a natural-language question from semantic context, and reads pixels only when visual proof is needed.',
  body: ({ msg, call, segment }) => {
    segment('1 · Retrieve', () => {
      msg('caller', 'api', 'POST /query', 'emphasis', 'question + optional time range');
      call('index', 'search + time range', 'relevant blocks');
      call('sdec', 'decode matching blocks', 'semantic context');
    });
    segment('2 · Generate', () => {
      call('llm', 'prompt + timeline', 'draft answer', { variant: 'emphasis' });
    });
    segment('3 · Verify + respond', () => {
      call('pdec', 'getFrame(t) if visual proof', 'frames', { variant: 'dashed' });
      msg('api', 'caller', 'answer + timestamps', 'return');
    });
  },
  cards: [
    { dot: 'cyan', title: 'How it works', items: [
      'Search finds the relevant time ranges and blocks',
      'Decoded blocks become a compact timeline for the LLM',
      'Far fewer tokens and no per-query vision model calls' ] },
    { dot: 'violet', title: 'Use case', items: [
      'What did the teacher write between 5 and 10 minutes?',
      'Answer cites timestamps; frames are fetched only to confirm' ] },
  ],
});
