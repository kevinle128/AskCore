---
status: accepted
---

# Refresh model metadata through a remote catalog overlay

Provider model metadata changes independently from Ask releases.
Keep a generated local baseline and add a remote metadata overlay with persisted cache, revalidation and offline operation, following Pi's catalog pattern.
This is preferred to a catalog that changes only through Ask releases or user edits.
Catalog membership remains separate from account access and does not establish permission to use a model.
Use Pi's public catalog as the initial remote source through an Ask schema adapter.
Keep the source base URL configurable, so Ask can replace the source without changing the provider domain contract.
Retain Ask endpoint/auth configuration and validate source/provider mapping rather than accepting remote routing changes without checks.
