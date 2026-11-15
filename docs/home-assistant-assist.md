# Ask Phoedox with Home Assistant Assist

Ask your documentation by voice: "Frag Phoedox, wo steht das NAS?" – Assist answers
"NAS ist hier: Keller, Rack."

Phoedox answers short questions without an AI provider (`POST /api/v1/projects/<project>/answer`):

| Question | Answer from |
|---|---|
| "Wo steht das NAS?" / "Where is the router?" | the entry's location (`located_in`, `part_of`, up to three steps) |
| "Welche IP hat die Fritzbox?" / "serial number of the printer" | a value of the entry (field name or label in the question) |
| "Fritzbox" | a short summary of the entry with up to three values |
| anything else | the best page of the full-text search, read out as a short snippet |

The entry is found by its name in the question. Secrets are never read out: they do not
live in entries, and fields whose names look like passwords, PINs, keys or tokens are skipped.

## 1. Create a read-only token

In Phoedox: profile menu → **API tokens** → new token, access **read**, only the project
you want to ask (e.g. `zuhause`). Put it into Home Assistant's `secrets.yaml`:

```yaml
phoedox_token: "Bearer phx_…"
```

## 2. Sentences

`config/custom_sentences/de/phoedox.yaml` (German; for English use `en/` and English sentences):

```yaml
language: "de"
intents:
  PhoedoxFrage:
    data:
      - sentences:
          - "frag phoedox {frage}"
          - "phoedox[,] {frage}"
lists:
  frage:
    wildcard: true
```

## 3. Call Phoedox and speak the answer

In `configuration.yaml` (replace the address and the project slug):

```yaml
rest_command:
  phoedox_answer:
    url: "https://app.phoedox.com/api/v1/projects/zuhause/answer"
    method: post
    headers:
      Authorization: !secret phoedox_token
    content_type: "application/json"
    payload: '{"q": {{ frage | tojson }}, "lang": "de"}'

intent_script:
  PhoedoxFrage:
    action:
      - action: rest_command.phoedox_answer
        data:
          frage: "{{ frage }}"
        response_variable: antwort
      - stop: ""
        response_variable: antwort
    speech:
      text: "{{ action_response.content.answer }}"
```

Restart Home Assistant (or reload the YAML configuration) and try it in the Assist dialog.

With the Phoedox add-on, use the add-on's address instead (e.g. `http://homeassistant.local:8080`).

## Answer format

```json
{"answer": "NAS ist hier: Keller.", "found": true, "source": "/p/zuhause/e/nas", "sourceTitle": "NAS"}
```

`found` is `false` when nothing matched; `answer` then says so in the chosen language (`de` or `en`).
