# ```request – HTTP request with checks

Runs an HTTP request from the page and checks the answer. People run any method from
the page; `run_request` runs only GET, HEAD and OPTIONS. `create_check` monitors a
reading block on a schedule.

````markdown
```request
GET {{base_url}}/api/status
Authorization: Bearer {{token}}

expect status == 200
expect $.state == "ok"
```
````

Each `expect` line is `<subject> <operator> <value>`:

| Subject | Means |
|---|---|
| `status` | HTTP status code |
| `time` | response time in ms |
| `header <Name>` | a response header, e.g. `header Content-Type contains json` |
| `$.a.b[0].c` | a JSON path into the body: `.field`, `[index]`, and `.length` at the end for arrays, strings and objects |

Operators: `==`, `!=`, `<`, `<=`, `>`, `>=` (numbers are compared as numbers),
`contains` (substring, or an array element), `exists`, `!exists` (no value).
Values are numbers, `"strings"`, `true`, `false`, `null` or bare text. Examples:
`expect time < 500`, `expect $.items.length >= 1`, `expect $.version exists`.

`{{variables}}` come from the project environment (`list_environment`; secret values
are never shown). Never put secrets into the block itself.
