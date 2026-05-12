# CRM HTTP Sidecar Contract

PAIMOS can talk to one configured HTTP CRM sidecar through the built-in
`http` CRM provider. This lets an operator bridge a CRM from any language
without compiling that provider into PAIMOS.

This document describes v1 of the wire contract. Payloads are JSON and use
snake_case field names. The machine-readable schema is
[`docs/schemas/crm-http-v1.json`](schemas/crm-http-v1.json).

## Provider Configuration

Configure the provider in Integrations -> CRM:

| Field | Required | Storage | Description |
| --- | --- | --- | --- |
| `base_url` | yes | plain config | Root URL of the sidecar. PAIMOS appends `/v1/...`. |
| `hmac_secret` | yes | encrypted secret config | Shared signing secret for request authentication. |
| `timeout_seconds` | no | plain config | Per-request timeout, 1-60 seconds. Default: 15. |

The current provider config model is keyed by `provider_id`, so v1 supports
one HTTP sidecar per PAIMOS deployment. Multiple named HTTP sidecars require a
future provider-instance model.

## Authentication

Every PAIMOS -> sidecar request includes:

```text
X-Paimos-Timestamp: <unix-seconds>
X-Paimos-Signature: <hex HMAC-SHA256(secret, timestamp + "\n" + raw_body)>
```

For `GET` requests, `raw_body` is the empty byte string. The sidecar should:

- reject missing headers,
- reject timestamps outside a +/- 300 second window,
- recompute the HMAC over the exact raw request body,
- compare the received and expected signatures in constant time.

Do not put secrets, signature bytes, or raw signed bodies in logs or error
responses.

## Endpoints

All endpoints live under the configured `base_url`.

```text
GET  /v1/schema
POST /v1/import
POST /v1/sync
POST /v1/search
GET  /v1/deep-link?id=<external_id>
```

### `GET /v1/schema`

Connection-test and metadata endpoint. PAIMOS signs the request and expects:

```json
{
  "version": "crm-http-v1",
  "name": "Example CRM bridge",
  "capabilities": ["import", "sync", "search", "deep_link"]
}
```

### `POST /v1/import`

Request:

```json
{ "ref": "customer-url-or-id-from-the-external-crm" }
```

Response:

```json
{
  "name": "Acme GmbH",
  "external_id": "crm-company-123",
  "external_url": "https://crm.example/companies/crm-company-123",
  "industry": "Security",
  "website": "https://acme.example",
  "contacts": [
    {
      "name": "Ada Admin",
      "email": "ada@acme.example",
      "is_primary": true,
      "external_id": "crm-contact-456"
    }
  ]
}
```

`name` and `external_id` are required. Empty strings mean "leave unset" for
optional customer fields.

### `POST /v1/sync`

Request:

```json
{ "external_id": "crm-company-123" }
```

Response:

```json
{
  "name": "Acme GmbH",
  "phone": "+43 1 234567",
  "external_url": "https://crm.example/companies/crm-company-123"
}
```

Every field is optional. Omitted or `null` fields are left unchanged in PAIMOS.
An explicit empty string clears that provider-owned field. `contacts` may be
omitted to leave contacts untouched.

### `POST /v1/search`

Request:

```json
{ "query": "acme", "limit": 10 }
```

Response:

```json
{
  "hits": [
    {
      "external_id": "crm-company-123",
      "name": "Acme GmbH",
      "industry": "Security",
      "external_url": "https://crm.example/companies/crm-company-123"
    }
  ]
}
```

`external_id` and `name` are required for every hit. PAIMOS adds
`already_imported` and `local_customer_id` itself after matching hits against
local customers; sidecars should not send those fields.

### `GET /v1/deep-link`

Request:

```text
GET /v1/deep-link?id=crm-company-123
```

Response:

```json
{ "url": "https://crm.example/companies/crm-company-123" }
```

## Customer Fields

`CustomerImport` and `PartialUpdate` share the same customer field names:

```text
name
contact_name
contact_email
address
country
industry
website
domain
vat_id
employee_count
annual_revenue_cents
description
phone
visit_address_street
visit_address_zip
external_url
contacts
```

`CustomerImport` also requires `external_id`. `PartialUpdate` does not include
`external_id`; PAIMOS already knows it from the linked customer row.

Contact fields:

```text
name
email
phone
role
is_primary
external_id
external_url
```

## Error Mapping

Sidecar status codes map to PAIMOS provider errors:

| Sidecar response | PAIMOS outcome |
| --- | --- |
| `2xx` | Success; JSON is decoded and contract-required fields are checked. |
| `400` | `ErrProviderBadRequest` |
| `401` / `403` | `ErrProviderAuth` |
| `404` | `ErrProviderNotFound` |
| `429` | Retry, then `ErrProviderUnreachable` if still failing. |
| `5xx` | Retry, then `ErrProviderUnreachable` if still failing. |
| Invalid JSON | `ErrProviderUnreachable` |
| Contract-invalid success body | `ErrProviderUnreachable` |

PAIMOS retries transient `429` and `5xx` responses up to three times after
the first attempt. It honors `Retry-After` when present and otherwise uses a
short exponential backoff.

## Out Of Scope

- CRM -> PAIMOS push/webhooks.
- Multiple configured HTTP sidecars in one PAIMOS deployment.
- Sidecar-specific admin config rendered inside PAIMOS.
