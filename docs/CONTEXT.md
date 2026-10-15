# Provider access language

These terms describe model access in Ask.

## Language

**Provider**:
A service that offers models and accepts one or more supported ways to authenticate.
_Avoid_: treating API-key access and subscription access as separate providers only because authentication differs.

**Model**:
A model offered through a provider.
The same model name can occur on several providers without identifying the same access choice.
_Avoid_: a globally unique model name.

**Wire API**:
The inference protocol used to communicate with a model-serving service.
_Avoid_: using a vendor name to identify a protocol.

**Auth method**:
A supported way to authenticate with a provider.
_Avoid_: assuming that every bearer token identifies subscription access.

**Subscription**:
A service plan that provides an allowance for model use.
_Avoid_: using subscription as a synonym for OAuth or for free inference.

**Known model**:
A model present in a provider's catalog.
_Avoid_: treating catalog membership as proof of account access.

**Account**:
The user's identity at a provider, such as the e-mail of a Claude or ChatGPT user.
_Avoid_: using account for a login option, for a subscription, or for the provider setup feature.

**Login option**:
A provider together with one of its auth methods, as the user selects it.
_Avoid_: account row, account method.

**Credential**:
The secret that Ask keeps to authenticate with a provider, either an API key or a sign-in token.
A provider has at most one credential, so a new credential replaces the old one, also when the auth method differs.
_Avoid_: login, account.

**Environment key**:
An API key that Ask reads from an environment variable and does not keep.
Ask uses it only when the provider has no credential, and removing the credential does not remove it.
_Avoid_: calling an environment key a credential.

**Usable provider**:
A provider that has a credential or an environment key.
_Avoid_: treating a usable provider as proof of account access, or calling it signed in.

**Sign-in**:
An interactive flow in which the user authenticates at the provider, and Ask gets a token as the credential.
_Avoid_: calling a key save a sign-in.

**Key save**:
The action in which the user gives Ask an API key, and Ask keeps it as the credential.
_Avoid_: sign-in, login.

**Account access**:
The permission of a particular account to use a model through a provider.
_Avoid_: treating a configured credential as proof of permission to use every known model.

**Credential removal**:
The action in which the user deletes the credential of a login option.
It does not remove an environment key.
For a sign-in token the user sees it as a sign-out; for an API key, as a key removal.
_Avoid_: calling the removal of an API key a logout.

**Default model**:
The model that the user chose for new sessions.
_Avoid_: using default for the model that the catalog recommends for a provider.

**Recommended model**:
The model that the catalog names for a provider when the user's choice cannot be used.
_Avoid_: provider default.

**Session model**:
The model that one session uses now.
_Avoid_: treating a change of the session model as a change of the default model.

**Fallback**:
A new session that starts on another model because the default model is not a known model or its provider is not a usable provider.
A fallback does not change the default model.
_Avoid_: treating a fallback as a new default.

**Request profile**:
The request and response rules required by a selected provider access path.
_Avoid_: treating all access paths for one provider as having identical rules.

**Tool contract**:
The tool's name, accepted input, execution behavior and returned result.
_Avoid_: treating a shared tool name as proof of identical input or behavior.
