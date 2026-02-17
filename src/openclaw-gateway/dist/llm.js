'use strict';

const https = require('https');
const http = require('http');

/**
 * Resolve provider config to a normalized { url, headers, bodyBuilder, parseStream } object.
 */
function resolveProvider(config) {
  const provider = (config.llm && config.llm.provider) || 'genai';
  const genai = (config.llm && config.llm.genai) || {};
  const modelOverride = config.llm && config.llm.model_override;

  switch (provider) {
    case 'tanzu_genai':
    case 'genai':
    case 'external_openai': {
      const base = (genai.endpoint || '').replace(/\/+$/, '');
      if (!base) throw new Error('LLM endpoint not configured (llm.genai.endpoint is empty)');
      const model = modelOverride || genai.model || 'auto';
      return {
        url: base + '/v1/chat/completions',
        headers: {
          'Content-Type': 'application/json',
          ...(genai.api_key ? { 'Authorization': 'Bearer ' + genai.api_key } : {})
        },
        buildBody(messages) {
          return JSON.stringify({ model, messages, stream: true });
        },
        parseStream: parseOpenAIStream
      };
    }

    case 'openai': {
      const apiKey = (config.llm && config.llm.openai && config.llm.openai.api_key) || '';
      if (!apiKey) throw new Error('OpenAI API key not configured');
      const model = modelOverride || 'gpt-4o';
      return {
        url: 'https://api.openai.com/v1/chat/completions',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + apiKey
        },
        buildBody(messages) {
          return JSON.stringify({ model, messages, stream: true });
        },
        parseStream: parseOpenAIStream
      };
    }

    case 'anthropic': {
      const apiKey = (config.llm && config.llm.anthropic && config.llm.anthropic.api_key) || '';
      if (!apiKey) throw new Error('Anthropic API key not configured');
      const model = modelOverride || 'claude-sonnet-4-5-20250929';
      return {
        url: 'https://api.anthropic.com/v1/messages',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01'
        },
        buildBody(messages) {
          // Convert from OpenAI format to Anthropic format
          const systemMsgs = messages.filter(m => m.role === 'system');
          const nonSystemMsgs = messages.filter(m => m.role !== 'system');
          const body = {
            model,
            max_tokens: 4096,
            stream: true,
            messages: nonSystemMsgs
          };
          if (systemMsgs.length > 0) {
            body.system = systemMsgs.map(m => m.content).join('\n\n');
          }
          return JSON.stringify(body);
        },
        parseStream: parseAnthropicStream
      };
    }

    default:
      throw new Error('Unknown LLM provider: ' + provider);
  }
}

/**
 * Parse OpenAI-compatible SSE stream.
 * Calls onToken(string) for each content delta, onDone() when complete.
 */
function parseOpenAIStream(chunk, buffer, onToken, onDone) {
  buffer.data += chunk;
  const lines = buffer.data.split('\n');
  buffer.data = lines.pop(); // keep incomplete line in buffer

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith('data: ')) continue;
    const payload = trimmed.slice(6);
    if (payload === '[DONE]') {
      onDone();
      return;
    }
    try {
      const parsed = JSON.parse(payload);
      const delta = parsed.choices && parsed.choices[0] && parsed.choices[0].delta;
      if (delta && delta.content) {
        onToken(delta.content);
      }
      // Check for finish_reason
      const finish = parsed.choices && parsed.choices[0] && parsed.choices[0].finish_reason;
      if (finish && finish !== 'null') {
        // Some providers send finish_reason without [DONE]
      }
    } catch (e) {
      // Skip unparseable lines
    }
  }
}

/**
 * Parse Anthropic SSE stream.
 * Calls onToken(string) for each content_block_delta, onDone() on message_stop.
 */
function parseAnthropicStream(chunk, buffer, onToken, onDone) {
  buffer.data += chunk;
  const lines = buffer.data.split('\n');
  buffer.data = lines.pop();

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith('data: ')) continue;
    const payload = trimmed.slice(6);
    try {
      const parsed = JSON.parse(payload);
      if (parsed.type === 'content_block_delta' && parsed.delta && parsed.delta.text) {
        onToken(parsed.delta.text);
      } else if (parsed.type === 'message_stop') {
        onDone();
        return;
      } else if (parsed.type === 'error') {
        throw new Error(parsed.error && parsed.error.message || 'Anthropic stream error');
      }
    } catch (e) {
      if (e.message && e.message.includes('stream error')) throw e;
      // Skip unparseable lines
    }
  }
}

/**
 * Stream a chat completion.
 * @param {object} config - Full openclaw.json config
 * @param {Array} messages - [{role, content}, ...]
 * @param {function} onToken - Called with each text token
 * @param {function} onDone - Called when stream is complete
 * @param {function} onError - Called on error
 */
function streamChat(config, messages, onToken, onDone, onError) {
  let provider;
  try {
    provider = resolveProvider(config);
  } catch (e) {
    onError(e);
    return;
  }

  const body = provider.buildBody(messages);
  const parsed = new URL(provider.url);
  const transport = parsed.protocol === 'https:' ? https : http;

  const reqOpts = {
    hostname: parsed.hostname,
    port: parsed.port || (parsed.protocol === 'https:' ? 443 : 80),
    path: parsed.pathname + parsed.search,
    method: 'POST',
    headers: {
      ...provider.headers,
      'Content-Length': Buffer.byteLength(body)
    }
  };

  const req = transport.request(reqOpts, (res) => {
    if (res.statusCode >= 400) {
      let errBody = '';
      res.on('data', (d) => { errBody += d.toString(); });
      res.on('end', () => {
        let msg = 'LLM API error (HTTP ' + res.statusCode + ')';
        try {
          const parsed = JSON.parse(errBody);
          msg += ': ' + (parsed.error && (parsed.error.message || parsed.error) || errBody.slice(0, 200));
        } catch (e) {
          msg += ': ' + errBody.slice(0, 200);
        }
        onError(new Error(msg));
      });
      return;
    }

    const buffer = { data: '' };
    res.setEncoding('utf8');
    res.on('data', (chunk) => {
      try {
        provider.parseStream(chunk, buffer, onToken, onDone);
      } catch (e) {
        onError(e);
      }
    });
    res.on('end', () => {
      // If stream ended without explicit done signal, call onDone
      onDone();
    });
    res.on('error', onError);
  });

  req.on('error', onError);
  req.write(body);
  req.end();
}

module.exports = { streamChat };
