import fs from 'node:fs';
const X = i => 40 + 198 * i;
const W = 140, H = 60;
const comps = [];
const SUB = {
  video: 'MP4 · lecture · camera', pose: 'gesture · action cues', ocr: 'PaddleOCR · text+bbox',
  agg: 'merge frame results', entity: 'entity builder · IoU', relation: 'near · holds · points',
  temporal: 'lifetimes · deltas', graph: 'entities + relations', comp: 'delta · dictionary',
  indexer: 'time · entity · vector', recon: 'entities + relations', unified: 'frames · semantics',
  rag: 'retrieve → context', agent: 'tools · pixel check', qa: 'answers + timestamps',
};
const add = (id, type, label, sublabel, col, y, extra = {}) =>
  comps.push({ id, type, label, sublabel: SUB[id] ?? sublabel, pos: [X(col), y], size: [W, H], ...extra });

// Ingestion
add('video', 'external', 'Input Video', 'MP4 · lecture · screen rec', 3, 40);
add('ingest', 'backend', 'Go Ingestion Engine', 'ffprobe + FFmpeg pipe', 3, 150);
add('sched', 'backend', 'Frame Scheduler', 'bounded buffer · PTS', 3, 260);
// Pixel lane
add('pixprep', 'cloud', 'Preprocessor', 'resize · normalize', 0, 370);
add('keyframe', 'cloud', 'Key Frame Detection', 'I-frame positions', 0, 480);
add('motion', 'cloud', 'Motion Processing', 'inter-frame prediction', 0, 590);
add('pixenc', 'cloud', 'H.264 / AV1 Encoder', 'FFmpeg libx264 · CRF', 0, 700);
add('pixstream', 'cloud', 'Pixel Stream', 'compressed H.264', 0, 810);
// Semantic sampling and job control
add('sampler', 'backend', 'Semantic Frame Sampler', 'interval · scene change', 3, 370);
add('queue', 'messagebus', 'Job Scheduler & Queue', 'Go · job status', 3, 480);
add('metrics', 'backend', 'Metrics & Logs', 'observability', 2, 480);
add('retry', 'backend', 'Retry Handler', 'backoff · recovery', 4, 480);
// ML workers
add('detector', 'frontend', 'Object Detector', 'YOLO · class + bbox', 1, 640);
add('ocr', 'frontend', 'OCR Engine', 'PaddleOCR · text + bbox', 2, 640);
add('scene', 'frontend', 'Scene / VLM', 'scene + context', 3, 640);
add('pose', 'frontend', 'Pose / Action Model', 'gesture · action features', 4, 640);
add('embed', 'frontend', 'Embedding Model', 'CLIP · text vectors', 5, 640);
// Semantic build
add('agg', 'backend', 'Semantic Aggregator', 'merge per-frame results', 3, 800);
add('entity', 'backend', 'Entity Tracker', 'IoU tracking · entity IDs', 3, 920);
add('action', 'backend', 'Action Recognition', 'rules + pose', 2, 1040);
add('event', 'backend', 'Event Detection', 'entered · left · changed', 3, 1040);
add('relation', 'backend', 'Relation Detection', 'near · holding · pointing', 4, 1040);
add('temporal', 'backend', 'Temporal Engine', 'lifetimes · dedup · deltas', 3, 1160);
add('graph', 'backend', 'Semantic Graph Builder', 'entities · events · relations', 3, 1280);
add('svir', 'backend', 'SVIR Block Generator', '8 semantic block types', 3, 1400);
add('comp', 'backend', 'Semantic Compressor', 'delta · dictionary · varint', 3, 1520);
add('indexer', 'backend', 'AI Index Builder', 'time · entity · text · vector', 4, 1520);
// Container + storage
add('container', 'database', 'SVC Container Writer', 'movie.svc', 3, 1680);
add('storage', 'database', 'Storage', 'Local FS · MinIO / S3', 3, 1790);
add('cache', 'database', 'Semantic Cache', 'hot semantic blocks', 2, 1790);
add('cdn', 'cloud', 'CDN / Streaming', 'delivery', 4, 1790);
// Decoder
add('reader', 'backend', 'SVC Reader', 'section table · CRC', 3, 1940);
add('pdec', 'cloud', 'Pixel Decoder', 'FFmpeg seek + decode', 2, 2050);
add('sdec', 'backend', 'Semantic Decoder', 'varint → SVIR', 3, 2050);
add('iread', 'backend', 'Index Reader', 'random access', 4, 2050);
add('recon', 'backend', 'Graph Reconstruction', 'entities · events · relations', 3, 2160);
add('sync', 'backend', 'Pixel / Semantic Sync', 'timestamp alignment', 3, 2270);
// API and AI
add('unified', 'database', 'Unified Video Object', 'frames · objects · timeline', 3, 2440);
add('api', 'backend', 'Semantic Video API', 'Go REST / gRPC', 3, 2560);
add('search', 'backend', 'Semantic Search', 'keyword + vector', 2, 2720);
add('rag', 'frontend', 'Semantic Video RAG', 'retrieve → compact context', 3, 2720);
add('llm', 'frontend', 'LLM / VLM', 'Claude · OpenAI · Ollama', 4, 2720);
add('agent', 'frontend', 'AI Agent', 'tools · pixel verification', 3, 2860);
add('qa', 'external', 'Video QA', 'answers with timestamps', 2, 3010);
add('summ', 'external', 'Summarization', 'timeline summaries', 4, 3010);

const c = (from, to, label, extra = {}) => ({ id: `${from}-to-${to}`, from, to, ...(label ? { label } : {}), ...extra });
const workers = ['detector', 'ocr', 'scene', 'pose', 'embed'];
const connections = [
  c('video', 'ingest', 'file'),
  c('ingest', 'sched', 'frames'),
  c('sched', 'pixprep', 'all frames', { fromSide: 'left', toSide: 'top', variant: 'emphasis' }),
  c('sched', 'sampler', 'sampled'),
  c('pixprep', 'keyframe'), c('keyframe', 'motion'), c('motion', 'pixenc'),
  c('pixenc', 'pixstream', 'H.264'),
  c('sampler', 'queue', 'jobs'),
  c('queue', 'metrics', 'stats', { fromSide: 'left', toSide: 'right', variant: 'dashed' }),
  c('queue', 'retry', 'fails', { fromSide: 'right', toSide: 'left', variant: 'dashed' }),
  ...workers.map(w => c('queue', w, null, { fromSide: 'bottom', toSide: 'top' })),
  ...workers.map(w => c(w, 'agg', null, { fromSide: 'bottom', toSide: 'top' })),
  c('agg', 'entity', 'detections'),
  c('entity', 'action'), c('entity', 'event'), c('entity', 'relation'),
  c('action', 'temporal'), c('event', 'temporal'), c('relation', 'temporal'),
  c('temporal', 'graph', 'timeline'),
  c('graph', 'svir'),
  c('svir', 'comp', 'blocks'),
  c('svir', 'indexer', 'blocks', { fromSide: 'right', toSide: 'top' }),
  c('comp', 'container', 'semantic stream'),
  c('indexer', 'container', 'indexes'),
  c('pixstream', 'container', 'pixel stream', { fromSide: 'bottom', toSide: 'left', variant: 'emphasis' }),
  c('container', 'storage', 'write'),
  c('storage', 'cache', 'warm', { fromSide: 'left', toSide: 'right', variant: 'dashed' }),
  c('storage', 'cdn', 'serve', { fromSide: 'right', toSide: 'left', variant: 'dashed' }),
  c('storage', 'reader', 'read'),
  c('reader', 'pdec', 'pixel'), c('reader', 'sdec', 'semantic'), c('reader', 'iread', 'index'),
  c('sdec', 'recon'),
  c('recon', 'sync'),
  c('pdec', 'sync'),
  c('iread', 'sync'),
  c('sync', 'unified'),
  c('unified', 'api'),
  c('api', 'search', 'search'), c('api', 'rag', 'query'), c('api', 'llm', 'context'),
  c('search', 'agent'), c('rag', 'agent'), c('llm', 'agent'),
  c('agent', 'qa'), c('agent', 'summ'),
];

const doc = {
  schema_version: 1,
  diagram_type: 'architecture',
  meta: {
    title: 'Semantic Video Codec — End-to-End Architecture',
    subtitle: 'Use case: let AI agents answer questions about a video from a compressed, indexed semantic stream instead of re-running vision models on every query.',
    output: 'semantic-video-codec.html',
    quality_profile: 'showcase',
    legend: { mode: 'auto', entries: {
      external: { label: 'Input / output' },
      backend: { label: 'Go core' },
      frontend: { label: 'Python ML / LLM' },
      cloud: { label: 'FFmpeg pixel path' },
      database: { label: 'Stored artifact' },
      messagebus: { label: 'Job queue' } } },
    views: [
      { id: 'ingest-pixels', label: 'Ingest & pixels', focus: ['video','ingest','sched','pixprep','keyframe','motion','pixenc','pixstream'], note: 'Every frame goes to the H.264 pixel path.' },
      { id: 'extraction', label: 'Semantic extraction', focus: ['sampler','queue','metrics','retry','detector','ocr','scene','pose','embed','agg'], note: 'Sampled frames run through Python models via gRPC.' },
      { id: 'understand', label: 'Understand & compress', focus: ['entity','action','event','relation','temporal','graph','svir','comp','indexer'], note: 'Detections become tracked entities, events and compact blocks.' },
      { id: 'container', label: 'Container & decode', focus: ['container','storage','cache','cdn','reader','pdec','sdec','iread','recon','sync'], note: 'One .svc file holds pixels, semantics and indexes.' },
      { id: 'serve', label: 'Serve AI', focus: ['unified','api','search','rag','llm','agent','qa','summ'], note: 'Agents query semantics first and fetch pixels only when needed.' },
    ],
  },
  components: comps,
  boundaries: [
    { kind: 'region', label: 'Pixel pipeline · FFmpeg', wraps: ['pixprep','keyframe','motion','pixenc','pixstream'] },
    { kind: 'region', label: 'Python ML services · gRPC', wraps: ['detector','ocr','scene','pose','embed'] },
    { kind: 'region', label: 'Semantic build · Go', wraps: ['agg','entity','action','event','relation','temporal','graph','svir','comp','indexer'] },
    { kind: 'region', label: 'SVC decoder · Go', wraps: ['reader','pdec','sdec','iread','recon','sync'] },
    { kind: 'region', label: 'AI consumption', wraps: ['search','rag','llm','agent'] },
  ],
  connections,
  cards: [
    { dot: 'cyan', title: 'Use case', items: [
      'Ask: "What did the teacher write between 5 and 10 minutes?"',
      'Search the index, read only those blocks, answer with timestamps',
      'Pixels are fetched only when visual proof is needed' ] },
    { dot: 'emerald', title: 'SVC container (movie.svc)', items: [
      'Header · Pixel stream · Semantic stream',
      'Temporal index · AI index · Metadata' ] },
    { dot: 'violet', title: 'Semantic block types', items: [
      'Entity · Object · Text · Action',
      'Event · Relation · Scene · Temporal' ] },
    { dot: 'amber', title: 'Tech stack', items: [
      'Go core · Python ML · FFmpeg H.264',
      'gRPC + Protobuf · MinIO / S3 · REST API' ] },
  ],
};
fs.writeFileSync('candidate.json', JSON.stringify(doc, null, 2));
