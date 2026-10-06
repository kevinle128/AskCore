---
status: accepted
---

# One provider with multiple auth methods

A provider can offer API-key access, subscription access, or both.
Represent it as one provider with an injected collection of supported auth methods, and select one method for each request.
This keeps the provider's model catalog and wire adapters shared while keeping method-specific request rules.
Following the user's clarification, store one type-tagged credential per provider, as Pi does.
Successful login with another auth method replaces that credential; supported methods do not imply simultaneous saved credentials.
Separate provider records describe independently configured services or endpoints; different authentication alone does not require another provider identity.

The rejected alternative was separate API-key and OAuth provider identities for the same service.
That alternative makes authentication changes look like provider changes and duplicates shared configuration.
