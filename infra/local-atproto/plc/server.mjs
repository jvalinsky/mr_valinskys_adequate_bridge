import { createServer } from 'node:http'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname } from 'node:path'

const port = parseInt(process.env.PLC_PORT || '2582', 10)
const dataFile = (process.env.PLC_DATA_FILE || '').trim()

/**
 * In-memory storage keyed by DID.
 * Value shape:
 * {
 *   did: string,
 *   doc: object,
 *   data: object,
 *   log: object[],
 *   tombstoned: boolean,
 *   updatedAt: string
 * }
 */
const state = new Map()

loadState()

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url || '/', `http://${req.headers.host || `127.0.0.1:${port}`}`)
    const pathname = decodeURIComponent(url.pathname)

    if (pathname === '/_health') {
      return sendJSON(res, 200, {
        status: 'ok',
        didCount: state.size,
      })
    }

    if (pathname === '/export') {
      const after = parseInt(url.searchParams.get('after') || '0', 10)
      const countRaw = url.searchParams.get('count')
      const count = countRaw ? Math.max(0, parseInt(countRaw, 10)) : undefined

      let seq = 1
      const rows = []
      for (const [did, entry] of state.entries()) {
        for (const op of entry.log) {
          rows.push({
            seq,
            type: 'sequenced_op',
            did,
            operation: op,
            cid: `bafylocal${String(seq).padStart(6, '0')}`,
            createdAt: entry.updatedAt,
          })
          seq++
        }
      }

      const filtered = rows.filter((row) => row.seq > after)
      const sliced = count === undefined ? filtered : filtered.slice(0, count)
      const ndjson = sliced.map((row) => JSON.stringify(row)).join('\n')
      res.writeHead(200, { 'content-type': 'application/x-ndjson' })
      res.end(ndjson)
      return
    }

    const did = firstPathSegment(pathname)
    if (!did || !did.startsWith('did:plc:')) {
      return sendJSON(res, 404, { error: 'Not Found' })
    }

    if (req.method === 'POST' && pathname === `/${did}`) {
      const body = await readJSONBody(req)
      if (!body || typeof body !== 'object') {
        return sendJSON(res, 400, { error: 'Invalid JSON body' })
      }

      const existing = state.get(did)
      const next = applyOperation(did, body, existing)
      state.set(did, next)
      persistState()
      return sendJSON(res, 200, { ok: true })
    }

    const entry = state.get(did)
    if (!entry) {
      return sendJSON(res, 404, { error: 'DID not found' })
    }

    if (entry.tombstoned) {
      return sendJSON(res, 404, { error: 'DID tombstoned' })
    }

    if (req.method === 'GET' && pathname === `/${did}`) {
      return sendJSON(res, 200, entry.doc)
    }

    if (req.method === 'GET' && pathname === `/${did}/data`) {
      return sendJSON(res, 200, entry.data)
    }

    if (req.method === 'GET' && pathname === `/${did}/log`) {
      return sendJSON(res, 200, entry.log)
    }

    if (req.method === 'GET' && pathname === `/${did}/log/last`) {
      const last = entry.log[entry.log.length - 1] || null
      if (!last) {
        return sendJSON(res, 404, { error: 'No operations for DID' })
      }
      return sendJSON(res, 200, last)
    }

    return sendJSON(res, 404, { error: 'Not Found' })
  } catch (err) {
    return sendJSON(res, 500, {
      error: 'Internal server error',
      message: err instanceof Error ? err.message : String(err),
    })
  }
})

server.listen(port, '0.0.0.0', () => {
  // stdout logging keeps this transparent in `docker compose logs`.
  process.stdout.write(`[local-plc] listening on 0.0.0.0:${port}\n`)
})

function applyOperation(did, op, existing) {
  const type = typeof op.type === 'string' ? op.type : ''

  if (type === 'plc_tombstone') {
    const prevLog = existing?.log || []
    return {
      did,
      doc: existing?.doc || null,
      data: existing?.data || null,
      log: [...prevLog, op],
      tombstoned: true,
      updatedAt: new Date().toISOString(),
    }
  }

  const data = normalizeDocumentData(did, op)
  const doc = didDocFromData(data)
  const prevLog = existing?.log || []
  return {
    did,
    doc,
    data,
    log: [...prevLog, op],
    tombstoned: false,
    updatedAt: new Date().toISOString(),
  }
}

function normalizeDocumentData(did, op) {
  if (op.type === 'create') {
    if (!isString(op.signingKey) || !isString(op.handle) || !isString(op.service)) {
      throw new Error('invalid create operation payload')
    }
    return {
      did,
      rotationKeys: isString(op.recoveryKey) ? [op.recoveryKey] : [],
      verificationMethods: { atproto: op.signingKey },
      alsoKnownAs: [`at://${op.handle}`],
      services: {
        atproto_pds: {
          type: 'AtprotoPersonalDataServer',
          endpoint: op.service,
        },
      },
    }
  }

  if (op.type === 'plc_operation') {
    if (!Array.isArray(op.alsoKnownAs) || typeof op.services !== 'object' || typeof op.verificationMethods !== 'object') {
      throw new Error('invalid plc_operation payload')
    }

    return {
      did,
      rotationKeys: Array.isArray(op.rotationKeys) ? op.rotationKeys.filter(isString) : [],
      verificationMethods: Object.fromEntries(
        Object.entries(op.verificationMethods || {}).filter((entry) => isString(entry[1])),
      ),
      alsoKnownAs: op.alsoKnownAs.filter(isString),
      services: normalizeServices(op.services),
    }
  }

  throw new Error(`unsupported operation type: ${String(op.type)}`)
}

function normalizeServices(services) {
  const out = {}
  for (const [serviceID, value] of Object.entries(services || {})) {
    if (!value || typeof value !== 'object') {
      continue
    }

    const endpoint = isString(value.endpoint)
      ? value.endpoint
      : isString(value.serviceEndpoint)
        ? value.serviceEndpoint
        : ''
    const type = isString(value.type) ? value.type : 'AtprotoPersonalDataServer'
    if (!endpoint) {
      continue
    }
    out[serviceID] = { type, endpoint }
  }
  return out
}

function didDocFromData(data) {
  const context = [
    'https://www.w3.org/ns/did/v1',
    'https://w3id.org/security/multikey/v1',
  ]

  const verificationMethod = Object.entries(data.verificationMethods).map(([keyID, key]) => ({
    id: `${data.did}#${keyID}`,
    type: 'Multikey',
    controller: data.did,
    publicKeyMultibase: stripDidKeyPrefix(key),
  }))

  const service = Object.entries(data.services).map(([serviceID, value]) => ({
    id: `#${serviceID}`,
    type: value.type,
    serviceEndpoint: value.endpoint,
  }))

  return {
    '@context': context,
    id: data.did,
    alsoKnownAs: data.alsoKnownAs,
    verificationMethod,
    service,
  }
}

function stripDidKeyPrefix(key) {
  if (!isString(key)) {
    return ''
  }
  if (key.startsWith('did:key:')) {
    return key.slice('did:key:'.length)
  }
  return key
}

function firstPathSegment(pathname) {
  const clean = pathname.replace(/^\/+/, '')
  const segment = clean.split('/')[0]
  if (!segment) {
    return ''
  }
  try {
    return decodeURIComponent(segment)
  } catch {
    return segment
  }
}

function sendJSON(res, statusCode, body) {
  const raw = JSON.stringify(body)
  res.writeHead(statusCode, {
    'content-type': 'application/json',
    'content-length': Buffer.byteLength(raw),
  })
  res.end(raw)
}

function readJSONBody(req) {
  return new Promise((resolve, reject) => {
    let raw = ''
    req.setEncoding('utf8')

    req.on('data', (chunk) => {
      raw += chunk
      if (raw.length > 1_000_000) {
        reject(new Error('request body too large'))
        req.destroy()
      }
    })

    req.on('end', () => {
      if (!raw.trim()) {
        resolve({})
        return
      }
      try {
        resolve(JSON.parse(raw))
      } catch (err) {
        reject(err)
      }
    })

    req.on('error', reject)
  })
}

function loadState() {
  if (!dataFile || !existsSync(dataFile)) {
    return
  }
  try {
    const parsed = JSON.parse(readFileSync(dataFile, 'utf8'))
    if (parsed && typeof parsed === 'object') {
      for (const [did, entry] of Object.entries(parsed)) {
        state.set(did, entry)
      }
    }
  } catch {
    // Keep startup resilient for local development.
  }
}

function persistState() {
  if (!dataFile) {
    return
  }
  const payload = Object.fromEntries(state.entries())
  mkdirSync(dirname(dataFile), { recursive: true })
  writeFileSync(dataFile, JSON.stringify(payload, null, 2))
}

function isString(value) {
  return typeof value === 'string' && value.length > 0
}
