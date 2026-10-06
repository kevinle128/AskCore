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

**Account access**:
The permission of a particular account to use a model through a provider.
_Avoid_: treating a configured credential as proof of permission to use every known model.

**Request profile**:
The request and response rules required by a selected provider access path.
_Avoid_: treating all access paths for one provider as having identical rules.

**Tool contract**:
The tool's name, accepted input, execution behavior and returned result.
_Avoid_: treating a shared tool name as proof of identical input or behavior.
