# Adding a New STT / TTS Provider (Frontend to Backend)

This guide covers the complete end-to-end steps for adding a new Speech-to-Text (STT) or Text-to-Speech (TTS) provider to Rapida, from the React UI through to the Go backend transformer.

---

## Overview of Touchpoints

Adding a new provider requires changes across these layers:

| Layer | Files | Purpose |
|-------|-------|---------|
| **Provider Metadata** | `ui/src/providers/<provider>/` | JSON data (models, voices, languages) |
| **Provider Registry** | `ui/src/providers/provider.production.json` + `.development.json` | Registers the provider with feature flags (`stt`, `tts`, `external`) |
| **Provider Exports** | `ui/src/providers/index.ts` | Accessor functions to load metadata JSON |
| **UI Config Component** | `ui/src/app/components/providers/speech-to-text/<provider>/` or `.../text-to-speech/<provider>/` | Provider-specific configuration form (model, language, voice dropdowns) |
| **UI Provider Router** | `ui/src/app/components/providers/speech-to-text/provider.tsx` or `.../text-to-speech/provider.tsx` | Switch-case routing to config component + defaults + validation |
| **Backend Transformer** | `api/assistant-api/internal/transformer/<provider>/` | Go implementation of `SpeechToTextTransformer` / `TextToSpeechTransformer` |
| **Backend Factory** | `api/assistant-api/internal/transformer/transformer.go` | Switch-case factory mapping provider string to constructor |

---

## Step 1: Register the Provider (Frontend)

### 1a. Add to Provider Registry JSON

Edit both `ui/src/providers/provider.production.json` and `ui/src/providers/provider.development.json`. Add an entry:

```json
{
    "code": "myprovider",
    "name": "My Provider",
    "description": "Description of what this provider offers.",
    "image": "https://cdn-01.rapida.ai/partners/myprovider.png",
    "featureList": ["stt", "tts", "external"],
    "configurations": [
        {
            "name": "key",
            "type": "string",
            "label": "API Key"
        }
    ],
    "website": "https://myprovider.com"
}
```

Key fields:
- **`code`**: Unique identifier used everywhere. Must match the `AudioTransformer` constant in the backend.
- **`featureList`**: Controls where the provider appears:
  - `"stt"` — shows in STT dropdown (filtered by `SPEECH_TO_TEXT_PROVIDER` in `index.ts`)
  - `"tts"` — shows in TTS dropdown (filtered by `TEXT_TO_SPEECH_PROVIDER`)
  - `"external"` — shows in integration/credential management
- **`configurations`**: Credential fields users provide when adding to their vault (e.g., API key, endpoint).

### 1b. Add Provider Metadata JSON Files

Create `ui/src/providers/<provider>/` with JSON files:

**For STT:**
- `speech-to-text-models.json` — available models
- `languages.json` or `speech-to-text-languages.json` — supported languages

**For TTS:**
- `voices.json` — available voices
- `text-to-speech-models.json` — models (if applicable)
- `languages.json` — supported languages

Example `speech-to-text-models.json`:
```json
[
    { "id": "model-v1", "name": "Model V1" },
    { "id": "model-v2", "name": "Model V2", "description": "Latest with improved accuracy." }
]
```

Example `voices.json`:
```json
[
    {
        "code": "voice-id-1",
        "name": "Alice",
        "gender": "feminine",
        "accent": "American",
        "locale": "en-us"
    }
]
```

### 1c. Export Accessors in `ui/src/providers/index.ts`

```typescript
// My Provider
export const MYPROVIDER_SPEECH_TO_TEXT_MODEL = () => {
  return require('./myprovider/speech-to-text-models.json');
};
export const MYPROVIDER_LANGUAGE = () => {
  return require('./myprovider/languages.json');
};
export const MYPROVIDER_VOICE = () => {
  return require('./myprovider/voices.json');
};
```

---

## Step 2: Create the UI Configuration Component (Frontend)

### 2a. Directory Structure

```
ui/src/app/components/providers/speech-to-text/<provider>/
├── index.tsx      # Config React component + re-exports
└── constant.ts    # Default options + validation logic
```

Same structure for TTS under `.../text-to-speech/<provider>/`.

### 2b. Implement `constant.ts` (Defaults + Validation)

```typescript
// ui/src/app/components/providers/speech-to-text/myprovider/constant.ts
import { MYPROVIDER_LANGUAGE, MYPROVIDER_SPEECH_TO_TEXT_MODEL } from '@/providers';
import { SetMetadata } from '@/utils/metadata';
import { Metadata } from '@rapidaai/react';

export const GetMyProviderDefaultOptions = (current: Metadata[]): Metadata[] => {
  const mtds: Metadata[] = [];
  const keysToKeep = ['rapida.credential_id', 'listen.language', 'listen.model'];

  const addMetadata = (
    key: string, defaultValue?: string, validationFn?: (value: string) => boolean,
  ) => {
    const metadata = SetMetadata(current, key, defaultValue, validationFn);
    if (metadata) mtds.push(metadata);
  };

  addMetadata('rapida.credential_id');
  addMetadata('listen.language', 'en-US', value =>
    MYPROVIDER_LANGUAGE().some(l => l.code === value),
  );
  addMetadata('listen.model', 'model-v2', value =>
    MYPROVIDER_SPEECH_TO_TEXT_MODEL().some(m => m.id === value),
  );

  return [
    ...mtds.filter(m => keysToKeep.includes(m.getKey())),
    ...current.filter(m => m.getKey().startsWith('microphone.')),
  ];
};

export const ValidateMyProviderOptions = (options: Metadata[]): string | undefined => {
  const credentialID = options.find(opt => opt.getKey() === 'rapida.credential_id');
  if (!credentialID || !credentialID.getValue() || credentialID.getValue().length === 0) {
    return 'Please provide a valid MyProvider credential for speech to text.';
  }
  const languageOption = options.find(opt => opt.getKey() === 'listen.language');
  if (!languageOption || !MYPROVIDER_LANGUAGE().some(l => l.code === languageOption.getValue())) {
    return 'Please provide a valid language for speech to text.';
  }
  const modelOption = options.find(opt => opt.getKey() === 'listen.model');
  if (!modelOption || !MYPROVIDER_SPEECH_TO_TEXT_MODEL().some(m => m.id === modelOption.getValue())) {
    return 'Please provide a valid model for speech to text.';
  }
  return undefined;
};
```

**Parameter naming conventions:**
- STT: `listen.<param>` (e.g., `listen.model`, `listen.language`, `listen.threshold`)
- TTS: `speak.<param>` (e.g., `speak.voice.id`, `speak.model`)
- Shared: `rapida.credential_id` (always required), `speaker.*` (TTS normalization), `microphone.*` (STT pipeline)

### 2c. Implement `index.tsx` (Config Component)

```tsx
// ui/src/app/components/providers/speech-to-text/myprovider/index.tsx
import { Metadata } from '@rapidaai/react';
import { Dropdown } from '@/app/components/dropdown';
import { FormLabel } from '@/app/components/form-label';
import { FieldSet } from '@/app/components/form/fieldset';
import { MYPROVIDER_SPEECH_TO_TEXT_MODEL, MYPROVIDER_LANGUAGE } from '@/providers';
export { GetMyProviderDefaultOptions, ValidateMyProviderOptions } from './constant';

const renderOption = (c: { icon: React.ReactNode; name: string }) => (
  <span className="inline-flex items-center gap-2 sm:gap-2.5 text-sm font-medium">
    {c.icon}
    <span className="truncate capitalize">{c.name}</span>
  </span>
);

export const ConfigureMyProviderSpeechToText: React.FC<{
  onParameterChange: (parameters: Metadata[]) => void;
  parameters: Metadata[] | null;
}> = ({ onParameterChange, parameters }) => {
  const getParamValue = (key: string) =>
    parameters?.find(p => p.getKey() === key)?.getValue() ?? '';

  const updateParameter = (key: string, value: string) => {
    const updatedParams = parameters ? parameters.map(p => p.clone()) : [];
    const idx = updatedParams.findIndex(p => p.getKey() === key);
    if (idx !== -1) {
      updatedParams[idx].setValue(value);
    } else {
      const newParam = new Metadata();
      newParam.setKey(key);
      newParam.setValue(value);
      updatedParams.push(newParam);
    }
    onParameterChange(updatedParams);
  };

  return (
    <>
      <FieldSet className="col-span-1 h-fit">
        <FormLabel>Model</FormLabel>
        <Dropdown
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={MYPROVIDER_SPEECH_TO_TEXT_MODEL().find(x => x.id === getParamValue('listen.model'))}
          setValue={v => updateParameter('listen.model', v.id)}
          allValue={MYPROVIDER_SPEECH_TO_TEXT_MODEL()}
          placeholder="Select model"
          option={renderOption}
          label={renderOption}
        />
      </FieldSet>
      <FieldSet className="col-span-1 h-fit">
        <FormLabel>Language</FormLabel>
        <Dropdown
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={MYPROVIDER_LANGUAGE().find(x => x.code === getParamValue('listen.language'))}
          setValue={v => updateParameter('listen.language', v.code)}
          allValue={MYPROVIDER_LANGUAGE()}
          placeholder="Select language"
          option={renderOption}
          label={renderOption}
        />
      </FieldSet>
    </>
  );
};
```

---

## Step 3: Wire the UI Config Component into the Provider Router

### 3a. For STT — Edit `ui/src/app/components/providers/speech-to-text/provider.tsx`

Add your provider to all three switch blocks:

1. **Import:**
```typescript
import {
  ConfigureMyProviderSpeechToText,
  GetMyProviderDefaultOptions,
  ValidateMyProviderOptions,
} from '@/app/components/providers/speech-to-text/myprovider';
```

2. **`GetDefaultSpeechToTextIfInvalid`** — add case:
```typescript
case 'myprovider':
  return GetMyProviderDefaultOptions(parameters);
```

3. **`ValidateSpeechToTextIfInvalid`** — add case:
```typescript
case 'myprovider':
  return ValidateMyProviderOptions(parameters);
```

4. **`SpeechToTextConfigComponent`** — add case:
```typescript
case 'myprovider':
  return (
    <ConfigureMyProviderSpeechToText
      parameters={parameters}
      onParameterChange={onChangeParameter}
    />
  );
```

### 3b. For TTS — Edit `ui/src/app/components/providers/text-to-speech/provider.tsx`

Same pattern — add to `GetDefaultTextToSpeechIfInvalid`, `ValidateTextToSpeechIfInvalid`, and `TextToSpeechConfigComponent`.

---

## Step 4: Implement the Backend Transformer (Go)

### 4a. Directory Structure

```
api/assistant-api/internal/transformer/<provider>/
├── <provider>.go       # Shared options/config struct
├── stt.go              # SpeechToTextTransformer implementation
├── tts.go              # TextToSpeechTransformer implementation
├── normalizer.go       # TextNormalizer for TTS
└── internal/           # (optional) internal callback types
    └── type.go
```

### 4b. Shared Options (`<provider>.go`)

```go
package internal_transformer_myprovider

import (
    "fmt"
    "github.com/rapidaai/pkg/commons"
    "github.com/rapidaai/pkg/utils"
    "github.com/rapidaai/protos"
)

type myProviderOption struct {
    key     string
    logger  commons.Logger
    mdlOpts utils.Option
}

func NewMyProviderOption(
    logger commons.Logger,
    vaultCredential *protos.VaultCredential,
    opts utils.Option,
) (*myProviderOption, error) {
    cx, ok := vaultCredential.GetValue().AsMap()["key"]
    if !ok {
        return nil, fmt.Errorf("illegal vault config")
    }
    return &myProviderOption{
        key:     cx.(string),
        logger:  logger,
        mdlOpts: opts,
    }, nil
}

func (o *myProviderOption) GetKey() string { return o.key }
```

The `opts utils.Option` map contains all `Metadata` key-value pairs from the frontend. Access with:
- `opts.GetString("listen.model")` — STT model
- `opts.GetString("listen.language")` — STT language
- `opts.GetString("speak.voice.id")` — TTS voice

### 4c. STT Transformer (`stt.go`)

Implements `internal_type.SpeechToTextTransformer` (embeds `Transformers[UserAudioPacket]`):

```go
package internal_transformer_myprovider

import (
    "context"
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    internal_type "github.com/rapidaai/api/assistant-api/internal/type"
    "github.com/rapidaai/pkg/commons"
    "github.com/rapidaai/pkg/utils"
    "github.com/rapidaai/protos"
)

type myProviderSTT struct {
    *myProviderOption
    mu            sync.Mutex
    ctx           context.Context
    ctxCancel     context.CancelFunc
    logger        commons.Logger
    client        interface{} // your provider's SDK client
    onPacket      func(pkt ...internal_type.Packet) error
    startedAtNano atomic.Int64
}

func (*myProviderSTT) Name() string {
    return "myprovider-speech-to-text"
}

func NewMyProviderSpeechToText(
    ctx context.Context,
    logger commons.Logger,
    credential *protos.VaultCredential,
    onPacket func(pkt ...internal_type.Packet) error,
    opts utils.Option,
) (internal_type.SpeechToTextTransformer, error) {
    providerOpts, err := NewMyProviderOption(logger, credential, opts)
    if err != nil {
        return nil, err
    }
    ct, cancel := context.WithCancel(ctx)
    return &myProviderSTT{
        ctx:              ct,
        ctxCancel:        cancel,
        logger:           logger,
        myProviderOption: providerOpts,
        onPacket:         onPacket,
    }, nil
}

func (m *myProviderSTT) Initialize() error {
    start := time.Now()

    // TODO: Create and connect to your provider's streaming API
    // e.g., WebSocket dial, gRPC stream, etc.
    // Start callback goroutine: go m.speechToTextCallback(client, m.ctx)

    // REQUIRED: emit initialized event
    m.onPacket(internal_type.ConversationEventPacket{
        Name: "stt",
        Data: map[string]string{
            "type":     "initialized",
            "provider": m.Name(),
            "init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
        },
        Time: time.Now(),
    })
    return nil
}

func (m *myProviderSTT) Transform(ctx context.Context, in internal_type.UserAudioPacket) error {
    m.mu.Lock()
    client := m.client
    m.mu.Unlock()
    if client == nil {
        return fmt.Errorf("myprovider-stt: not initialized")
    }

    // Record first audio chunk time for latency tracking
    m.startedAtNano.CompareAndSwap(0, time.Now().UnixNano())

    // TODO: Send in.Audio ([]byte, 16kHz linear16) to your provider
    return nil
}

func (m *myProviderSTT) Close(ctx context.Context) error {
    m.ctxCancel()
    m.mu.Lock()
    defer m.mu.Unlock()
    // TODO: Close your client connection
    return nil
}
```

### 4d. TTS Transformer (`tts.go`)

Implements `internal_type.TextToSpeechTransformer` (embeds `Transformers[LLMPacket]`):

```go
package internal_transformer_myprovider

import (
    "context"
    "fmt"
    "sync"
    "time"

    internal_type "github.com/rapidaai/api/assistant-api/internal/type"
    "github.com/rapidaai/pkg/commons"
    "github.com/rapidaai/pkg/utils"
    "github.com/rapidaai/protos"
)

type myProviderTTS struct {
    *myProviderOption
    ctx           context.Context
    ctxCancel     context.CancelFunc
    contextId     string
    mu            sync.Mutex
    ttsStartedAt  time.Time
    ttsMetricSent bool
    logger        commons.Logger
    connection    interface{} // your provider's client
    onPacket      func(pkt ...internal_type.Packet) error
    normalizer    internal_type.TextNormalizer
}

func NewMyProviderTextToSpeech(
    ctx context.Context,
    logger commons.Logger,
    credential *protos.VaultCredential,
    onPacket func(pkt ...internal_type.Packet) error,
    opts utils.Option,
) (internal_type.TextToSpeechTransformer, error) {
    providerOpts, err := NewMyProviderOption(logger, credential, opts)
    if err != nil {
        return nil, err
    }
    ct, cancel := context.WithCancel(ctx)
    return &myProviderTTS{
        myProviderOption: providerOpts,
        ctx:              ct,
        ctxCancel:        cancel,
        logger:           logger,
        onPacket:         onPacket,
        normalizer:       NewMyProviderNormalizer(logger, opts),
    }, nil
}

func (*myProviderTTS) Name() string {
    return "myprovider-text-to-speech"
}

func (t *myProviderTTS) Initialize() error {
    start := time.Now()

    // TODO: Connect to your provider's streaming TTS API
    // Start callback goroutine: go t.textToSpeechCallback(conn, t.ctx)

    // REQUIRED: emit initialized event
    t.onPacket(internal_type.ConversationEventPacket{
        Name: "tts",
        Data: map[string]string{
            "type":     "initialized",
            "provider": t.Name(),
            "init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
        },
        Time: time.Now(),
    })
    return nil
}

func (t *myProviderTTS) Transform(ctx context.Context, in internal_type.LLMPacket) error {
    t.mu.Lock()
    conn := t.connection
    if in.ContextId() != t.contextId {
        t.contextId = in.ContextId()
        t.ttsStartedAt = time.Time{}
        t.ttsMetricSent = false
    }
    t.mu.Unlock()

    if conn == nil {
        return fmt.Errorf("myprovider-tts: not initialized")
    }

    switch input := in.(type) {
    case internal_type.InterruptionPacket:
        // Handle interruption: clear buffered audio, reset metrics
        t.mu.Lock()
        t.ttsStartedAt = time.Time{}
        t.ttsMetricSent = false
        t.mu.Unlock()
        // REQUIRED: emit interrupted event
        t.onPacket(internal_type.ConversationEventPacket{
            Name: "tts",
            Data: map[string]string{"type": "interrupted"},
            Time: time.Now(),
        })
        return nil

    case internal_type.LLMResponseDeltaPacket:
        // Track TTS start time for latency metric
        t.mu.Lock()
        if t.ttsStartedAt.IsZero() {
            t.ttsStartedAt = time.Now()
        }
        t.mu.Unlock()

        // Normalize text and send to provider
        normalized := t.normalizer.Normalize(ctx, input.Text)
        // TODO: Send normalized text to your provider

        // REQUIRED: emit speaking event
        t.onPacket(internal_type.ConversationEventPacket{
            Name: "tts",
            Data: map[string]string{"type": "speaking", "text": normalized},
            Time: time.Now(),
        })
        return nil

    case internal_type.LLMResponseDonePacket:
        // Signal end of text: flush remaining audio from provider
        // TODO: Send flush/close signal to provider
        return nil

    default:
        return fmt.Errorf("myprovider-tts: unsupported input type %T", in)
    }
}

func (t *myProviderTTS) Close(ctx context.Context) error {
    t.ctxCancel()
    t.mu.Lock()
    defer t.mu.Unlock()
    // TODO: Close your provider connection
    return nil
}
```

### 4e. Text Normalizer (`normalizer.go`)

```go
package internal_transformer_myprovider

import (
    "context"
    "strings"

    internal_normalizers "github.com/rapidaai/api/assistant-api/internal/normalizers"
    internal_type "github.com/rapidaai/api/assistant-api/internal/type"
    "github.com/rapidaai/pkg/commons"
    "github.com/rapidaai/pkg/utils"
)

type myProviderNormalizer struct {
    logger      commons.Logger
    config      internal_type.NormalizerConfig
    language    string
    normalizers []internal_normalizers.Normalizer
}

func NewMyProviderNormalizer(logger commons.Logger, opts utils.Option) internal_type.TextNormalizer {
    cfg := internal_type.DefaultNormalizerConfig()
    language, _ := opts.GetString("speaker.language")
    if language == "" {
        language = "en"
    }
    var normalizers []internal_normalizers.Normalizer
    if dictionaries, err := opts.GetString("speaker.pronunciation.dictionaries"); err == nil && dictionaries != "" {
        normalizerNames := strings.Split(dictionaries, commons.SEPARATOR)
        normalizers = internal_type.BuildNormalizerPipeline(logger, normalizerNames)
    }
    return &myProviderNormalizer{
        logger: logger, config: cfg, language: language, normalizers: normalizers,
    }
}

func (n *myProviderNormalizer) Normalize(ctx context.Context, text string) string {
    if text == "" {
        return text
    }
    for _, normalizer := range n.normalizers {
        text = normalizer.Normalize(text)
    }
    // Add provider-specific normalization if needed (e.g., SSML for Azure/Google)
    return strings.TrimSpace(text)
}
```

---

## Step 5: Required Events (ConversationEventPacket)

Every transformer MUST emit `ConversationEventPacket` via `onPacket` at specific lifecycle points. These events power the debugger UI, observability, and metrics. The `Name` field is always `"stt"` or `"tts"`.

### 5a. STT Events

| When | Event `Data["type"]` | Additional `Data` fields | Also emit |
|------|---------------------|--------------------------|-----------|
| **Connection established** | `"initialized"` | `"provider"`, `"init_ms"` | — |
| **Final transcript received** | `"completed"` | `"script"`, `"confidence"`, `"language"`, `"word_count"`, `"char_count"` | `InterruptionPacket{Source: "word"}` + `SpeechToTextPacket{Interim: false}` + `MessageMetricPacket{stt_latency_ms}` |
| **Interim transcript received** | `"interim"` | `"script"`, `"confidence"` | `InterruptionPacket{Source: "word"}` + `SpeechToTextPacket{Interim: true}` |
| **Transcript below confidence threshold** | `"low_confidence"` | `"script"`, `"confidence"`, `"threshold"` | nothing else (skip processing) |
| **Error from provider** | `"error"` | `"error"` (message string) | — |

**Full STT callback packet sequence** (reference: `deepgram/internal/stt_callback.go`):

```go
// On final transcript:
m.onPacket(
    internal_type.InterruptionPacket{Source: "word"},
    internal_type.SpeechToTextPacket{
        Script:     transcript,
        Confidence: confidence,
        Language:   language,
        Interim:    false,
    },
    internal_type.ConversationEventPacket{
        Name: "stt",
        Data: map[string]string{
            "type":       "completed",
            "script":     transcript,
            "confidence": fmt.Sprintf("%.4f", confidence),
            "language":   language,
            "word_count": fmt.Sprintf("%d", len(strings.Fields(transcript))),
            "char_count": fmt.Sprintf("%d", len(transcript)),
        },
        Time: time.Now(),
    },
    internal_type.MessageMetricPacket{
        Metrics: []*protos.Metric{{
            Name:  "stt_latency_ms",
            Value: fmt.Sprintf("%d", latencyMs),
        }},
    },
)

// On interim transcript:
m.onPacket(
    internal_type.InterruptionPacket{Source: "word"},
    internal_type.SpeechToTextPacket{
        Script:     transcript,
        Confidence: confidence,
        Language:   language,
        Interim:    true,
    },
    internal_type.ConversationEventPacket{
        Name: "stt",
        Data: map[string]string{
            "type":       "interim",
            "script":     transcript,
            "confidence": fmt.Sprintf("%.4f", confidence),
        },
        Time: time.Now(),
    },
)

// On low confidence (below threshold):
m.onPacket(
    internal_type.ConversationEventPacket{
        Name: "stt",
        Data: map[string]string{
            "type":       "low_confidence",
            "script":     transcript,
            "confidence": fmt.Sprintf("%.4f", confidence),
            "threshold":  fmt.Sprintf("%.4f", threshold),
        },
        Time: time.Now(),
    },
)

// On error:
m.onPacket(internal_type.ConversationEventPacket{
    Name: "stt",
    Data: map[string]string{"type": "error", "error": err.Error()},
    Time: time.Now(),
})
```

**STT latency metric**: Measured from the first `Transform()` call (first audio chunk sent) to when the final transcript is received. Use `atomic.Int64` to store `time.Now().UnixNano()` on first chunk, then swap-and-calculate in the callback.

### 5b. TTS Events

| When | Event `Data["type"]` | Additional `Data` fields | Also emit |
|------|---------------------|--------------------------|-----------|
| **Connection established** | `"initialized"` | `"provider"`, `"init_ms"` | — |
| **Text chunk sent to provider** | `"speaking"` | `"text"` (the normalized text) | — |
| **All audio for a turn finished** | `"completed"` | — | `TextToSpeechEndPacket` |
| **Interruption handled** | `"interrupted"` | — | — |

**Full TTS callback packet sequences:**

```go
// In the TTS callback goroutine, when receiving audio chunks from the provider:

// 1. First audio chunk — emit TTS latency metric (once per turn):
t.onPacket(internal_type.MessageMetricPacket{
    ContextID: ctxId,
    Metrics: []*protos.Metric{{
        Name:  "tts_latency_ms",
        Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
    }},
})

// 2. Every audio chunk:
t.onPacket(internal_type.TextToSpeechAudioPacket{
    ContextID:  ctxId,
    AudioChunk: audioBytes, // raw PCM audio, 16kHz linear16
})

// 3. When all audio for the turn is done (flush/end signal from provider):
t.onPacket(
    internal_type.TextToSpeechEndPacket{ContextID: ctxId},
    internal_type.ConversationEventPacket{
        Name: "tts",
        Data: map[string]string{"type": "completed"},
        Time: time.Now(),
    },
)
```

**TTS latency metric**: Measured from when the first `LLMResponseDeltaPacket` arrives in `Transform()` (record `ttsStartedAt`) to when the first audio chunk comes back from the provider. Emit once per turn using a `ttsMetricSent` boolean flag.

### 5c. Event Summary Diagram

```
STT Lifecycle:
  Initialize() → event{type: "initialized"}
  Transform() calls (audio streaming)...
    Provider callback:
      interim result  → InterruptionPacket + SpeechToTextPacket{Interim:true}  + event{type: "interim"}
      final result    → InterruptionPacket + SpeechToTextPacket{Interim:false} + event{type: "completed"} + MessageMetricPacket{stt_latency_ms}
      low confidence  → event{type: "low_confidence"}
      error           → event{type: "error"}
  Close()

TTS Lifecycle:
  Initialize() → event{type: "initialized"}
  Transform(InterruptionPacket)      → clear buffer + event{type: "interrupted"}
  Transform(LLMResponseDeltaPacket)  → normalize + send text + event{type: "speaking"}
  Transform(LLMResponseDonePacket)   → flush
    Provider callback:
      first audio chunk → MessageMetricPacket{tts_latency_ms} (once)
      each audio chunk  → TextToSpeechAudioPacket
      done/flushed      → TextToSpeechEndPacket + event{type: "completed"}
  Close()
```

---

## Step 6: Register in the Backend Factory

### Edit `api/assistant-api/internal/transformer/transformer.go`

1. **Add import:**
```go
internal_transformer_myprovider "github.com/rapidaai/api/assistant-api/internal/transformer/myprovider"
```

2. **Add constant** (must match `code` from provider JSON):
```go
const (
    // ... existing
    MYPROVIDER AudioTransformer = "myprovider"
)
```

3. **Add case to `GetTextToSpeechTransformer`** (if TTS):
```go
case MYPROVIDER:
    return internal_transformer_myprovider.NewMyProviderTextToSpeech(ctx, logger, credential, onPacket, opts)
```

4. **Add case to `GetSpeechToTextTransformer`** (if STT):
```go
case MYPROVIDER:
    return internal_transformer_myprovider.NewMyProviderSpeechToText(ctx, logger, credential, onPacket, opts)
```

---

## Step 7: Test

### Backend
```bash
go test ./api/assistant-api/internal/transformer/myprovider/...
```

### Frontend
```bash
cd ui && yarn test
```

### Integration
1. `make up-all`
2. Add provider credentials in Integrations > Vault
3. Create/edit an assistant deployment (Phone / Web / Debugger)
4. Select your provider from STT or TTS dropdown
5. Configure model/voice/language
6. Deploy and test with a live conversation
7. Verify events appear in the Debugger UI

---

## Quick Reference: Data Flow

```
User speaks
  → Audio captured (WebRTC/SIP)
    → UserAudioPacket
      → STT.Transform(ctx, UserAudioPacket)
        → Provider API (WebSocket/gRPC/REST)
          → Callback emits: SpeechToTextPacket + events + metrics
            → EOS detection → LLM execution
              → LLM streams LLMResponseDeltaPacket
                → TTS.Transform(ctx, LLMPacket)
                  → Provider API (text → audio)
                    → Callback emits: TextToSpeechAudioPacket + events + metrics
                      → Streamer → User hears audio
```

---

## Checklist

- [ ] Provider entry in `provider.production.json` and `provider.development.json` with correct `code` and `featureList`
- [ ] Provider metadata JSON files created (`models`, `voices`, `languages`)
- [ ] Accessor functions exported from `ui/src/providers/index.ts`
- [ ] STT config component created (`index.tsx` + `constant.ts`) with defaults and validation
- [ ] TTS config component created (`index.tsx` + `constant.ts`) with defaults and validation
- [ ] Config components wired into `provider.tsx` (all 3 switch blocks per STT/TTS)
- [ ] Backend transformer directory created with `<provider>.go`, `stt.go`, `tts.go`, `normalizer.go`
- [ ] `SpeechToTextTransformer` interface implemented (`Name`, `Initialize`, `Transform`, `Close`)
- [ ] `TextToSpeechTransformer` interface implemented (`Name`, `Initialize`, `Transform`, `Close`)
- [ ] **All required events emitted** (see Step 5):
  - [ ] STT: `initialized`, `completed` (with metric), `interim`, `low_confidence`, `error`
  - [ ] TTS: `initialized`, `speaking`, `completed`, `interrupted`
- [ ] `SpeechToTextPacket` emitted with correct `Script`, `Confidence`, `Language`, `Interim` fields
- [ ] `TextToSpeechAudioPacket` emitted with raw PCM audio chunks
- [ ] `TextToSpeechEndPacket` emitted when turn audio is complete
- [ ] `InterruptionPacket{Source: "word"}` emitted before every `SpeechToTextPacket`
- [ ] `MessageMetricPacket` emitted for `stt_latency_ms` (final transcripts) and `tts_latency_ms` (first audio chunk)
- [ ] Text normalizer implemented for TTS
- [ ] Provider constant and factory cases added in `transformer.go`
- [ ] Provider `code` string matches across all layers (JSON, UI switch-cases, Go constant)
- [ ] Tests written for backend transformer
- [ ] Integration tested end-to-end
