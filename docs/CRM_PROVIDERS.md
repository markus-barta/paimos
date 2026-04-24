# Adding a CRM Provider

PAIMOS owns its own customer data model (PAI-28). External CRMs
(HubSpot, Pipedrive, Salesforce, Attio, …) are **optional** sync
sources, plugged in through a small Go interface. This page is the
developer guide for adding a new in-tree provider.

> **Audience:** self-hosted PAIMOS maintainers and contributors who
> want to wire a CRM that doesn't ship in upstream. If you just want
> to *use* an existing provider (e.g. HubSpot), look in
> Settings → CRM in the app.

---

## 1. Why pluggable

PAIMOS designs for three audiences ([PAI-28]):

1. **No-CRM users** — manual customer entry is a primary mode, not a
   fallback. The plugin layer is opt-in.
2. **HubSpot users** — one-shot import + manual re-sync + deep-link
   via the in-tree HubSpot provider.
3. **Other-CRM users** — write a Go provider against the
   `crm.Provider` interface; no fork of core PAIMOS, no schema
   change.

Provider semantics live in one place: the registry. Adding HubSpot,
Pipedrive, or your internal CRM is the same shape — register a Go
type that satisfies `crm.Provider`, and the rest of the app
(sidebar, customer list, detail header, sync button, admin
Integrations tab) lights up via the existing
`useExternalProvider` composable.

[PAI-28]: https://pm.barta.cm/projects/PAI/issues/PAI-28

---

## 2. Interface walkthrough

The contract is in [`backend/handlers/crm/provider.go`](../backend/handlers/crm/provider.go).
Every method is exercised by the generic HTTP handlers in
`backend/handlers/crm/handlers.go`, so once your type satisfies the
interface, the API and UI work without further changes.

```go
type Provider interface {
    ID() string                                                      // stable; never rename once shipped
    Name() string                                                    // human display name
    LogoURL() string                                                 // path or URL; "" → globe fallback
    ConfigSchema() ConfigSchema                                      // fields the admin UI renders
    ValidateConfig(values map[string]string) error                   // surface errors to admin
    ImportRef(ctx, rawRef string, cfg ProviderConfig) (CustomerImport, error)
    Sync(ctx, externalID string, cfg ProviderConfig) (PartialUpdate, error)
    DeepLink(externalID string, cfg ProviderConfig) string
}
```

Two payload types:

- `CustomerImport` carries the field set the provider can populate on
  initial import (`Name`, `Industry`, `Address`, `Country`,
  `ContactName`, `ContactEmail`, plus `ExternalID` / `ExternalURL`).
  Empty strings = leave unset.
- `PartialUpdate` is what `Sync` returns — same fields in pointer
  form. The generic sync handler PATCHes only fields the provider
  actually wrote, so PAIMOS-only fields like `rate_hourly` and
  `notes` are **never** clobbered by an upstream change.

---

## 3. Skeleton: copy / paste / rename

Create `backend/handlers/crm/<provider>/provider.go`:

```go
// Package <provider> is a CRMProvider implementation for <CRM Name>.
package pipedrive

import (
    "context"
    "github.com/markus-barta/paimos/backend/handlers/crm"
)

func init() { crm.Register(&Provider{}) }

type Provider struct{}

func (p *Provider) ID() string      { return "pipedrive" }
func (p *Provider) Name() string    { return "Pipedrive" }
func (p *Provider) LogoURL() string { return "/assets/crm/pipedrive.svg" }

func (p *Provider) ConfigSchema() crm.ConfigSchema {
    return crm.ConfigSchema{Fields: []crm.ConfigField{
        {Key: "token",      Label: "API Token", Type: "secret", Required: true,
         Help: "Personal API token from Pipedrive → Settings → Personal preferences → API."},
        {Key: "company",    Label: "Company subdomain", Type: "string", Required: true,
         Help: "The <subdomain> in https://<subdomain>.pipedrive.com.",
         Placeholder: "acme"},
    }}
}

func (p *Provider) ValidateConfig(values map[string]string) error {
    if values["token"] == "" || values["company"] == "" {
        return errors.New("token and company subdomain are both required")
    }
    return nil
}

func (p *Provider) ImportRef(ctx context.Context, rawRef string, cfg crm.ProviderConfig) (crm.CustomerImport, error) {
    orgID, err := resolveOrgID(rawRef)
    if err != nil {
        return crm.CustomerImport{}, err
    }
    org, err := fetchOrg(ctx, cfg.Get("company"), cfg.Get("token"), orgID)
    if err != nil {
        return crm.CustomerImport{}, err
    }
    return crm.CustomerImport{
        Name:        org.Name,
        Industry:    org.Industry,
        Address:     org.Address,
        Country:     org.Country,
        ExternalID:  org.ID,
        ExternalURL: p.DeepLink(org.ID, cfg),
    }, nil
}

func (p *Provider) Sync(ctx context.Context, externalID string, cfg crm.ProviderConfig) (crm.PartialUpdate, error) {
    org, err := fetchOrg(ctx, cfg.Get("company"), cfg.Get("token"), externalID)
    if err != nil {
        return crm.PartialUpdate{}, err
    }
    return crm.PartialUpdate{
        Name:     &org.Name,
        Industry: &org.Industry,
        Address:  &org.Address,
        Country:  &org.Country,
    }, nil
}

func (p *Provider) DeepLink(externalID string, cfg crm.ProviderConfig) string {
    company := cfg.Get("company")
    if company == "" || externalID == "" {
        return ""
    }
    return fmt.Sprintf("https://%s.pipedrive.com/organization/%s", company, externalID)
}
```

Then blank-import the package from `backend/main.go` so its `init()`
fires before routes are registered:

```go
import (
    // CRM provider plugins. One blank import per compiled-in provider.
    _ "github.com/markus-barta/paimos/backend/handlers/crm/hubspot"
    _ "github.com/markus-barta/paimos/backend/handlers/crm/pipedrive"
)
```

That's it. The next `just deploy` ships your provider; the admin sees
a new card in Settings → CRM, configures it, enables it, and customer
imports route through it automatically.

---

## 4. Config schema field types

`ConfigField.Type` drives both rendering in the admin UI and the
storage path the plugin layer uses:

| Type     | Storage                      | Admin UI                                              |
|----------|------------------------------|-------------------------------------------------------|
| `string` | plain JSON, `config_json`    | `<input type="text">`                                 |
| `number` | plain JSON, `config_json`    | `<input type="number">`                               |
| `select` | plain JSON, `config_json`    | `<select>` populated from `Options`                   |
| `secret` | AES-GCM, `config_secret_json`| password input; never echoed; "Replace" / "Clear" UI  |

Only mark a field `secret` if it is actually a credential. Plain
strings are visible to the admin tab and to the diagnostic logs;
secrets are not.

---

## 5. Error handling conventions

Return `*crm.ProviderError` with a `Kind` from this set so the
generic handler maps to a sensible HTTP status:

| `Kind`                       | HTTP | When to use                                          |
|------------------------------|------|------------------------------------------------------|
| `ErrProviderUnreachable`     | 502  | Network failure; `http.Client` returned an error     |
| `ErrProviderAuth`            | 401  | Upstream returned 401 / 403                          |
| `ErrProviderNotFound`        | 404  | Upstream returned 404 for the requested entity       |
| `ErrProviderBadRequest`      | 400  | The user-supplied `ref` couldn't be parsed           |
| `ErrProviderUnknown` (zero)  | 500  | Anything else; details go to the server log only     |

The generic handler also accepts plain `error` values; those become
`500` with the error string surfaced to the admin. Prefer typed
errors so the UI can render differentiated messaging.

**Never** include the raw token / API key in an error message — the
HTTP layer surfaces the message string verbatim to the admin client.

---

## 6. Contract test harness

A minimal contract test lives in
`backend/handlers/crm/contract_test.go` (TODO — file when the second
provider lands; until then the HubSpot integration tests are the de
facto contract). The eventual harness will:

- Boot a fake HTTP upstream
- Run your provider's `ImportRef` against it with a known reference
- Assert the returned `CustomerImport` matches a fixture
- Repeat for `Sync`
- Verify `DeepLink` is constructed with the expected pattern

To opt in, add a test that calls `crm.Reset()`, `crm.Register(&Provider{})`,
and a few table-driven calls.

---

## 7. Future: HTTP-based external providers

The current plugin layer is in-process Go. Adding a provider requires
recompiling PAIMOS — fine for the maintainer's own use and for any
self-hosted Go-friendly user.

For non-Go users, [PAI-108] is filed as deferred: an HTTP contract
that lets a sidecar service plug in over the wire, via a single
in-tree `http` provider that speaks the same JSON shape as this
interface but transports it across HTTP. It's deliberately deferred
until the in-process layer + at least one production provider
(HubSpot, [PAI-56]) have soak time, so the contract is informed by a
real implementation rather than designed in a vacuum.

If you have a use case that pushes us to start sooner, please open an
issue with the workflow you need so we can validate the contract
sketch against it.

[PAI-56]:  https://pm.barta.cm/projects/PAI/issues/PAI-56
[PAI-108]: https://pm.barta.cm/projects/PAI/issues/PAI-108
