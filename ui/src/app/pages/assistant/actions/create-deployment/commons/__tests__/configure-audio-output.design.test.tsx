import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { Metadata } from '@rapidaai/react';
import { ConfigureAudioOutputProvider } from '../configure-audio-output';

const mockGetDefaultSpeakerConfig = jest.fn();
const mockGetDefaultTextToSpeechIfInvalid = jest.fn();

jest.mock('@/utils', () => ({
  cn: (...inputs: any[]) => inputs.filter(Boolean).join(' '),
}));

jest.mock('lucide-react', () => ({
  ChevronDown: () => null,
}));

jest.mock('@/app/components/blocks/section-divider', () => ({
  SectionDivider: ({ label }: { label: string }) => <div>{label}</div>,
}));

jest.mock('@/app/components/providers/text-to-speech', () => ({
  TextToSpeechProvider: ({
    onChangeProvider,
  }: {
    onChangeProvider: (provider: string) => void;
  }) => (
    <button onClick={() => onChangeProvider('openai')} type="button">
      change tts
    </button>
  ),
}));

jest.mock('@/app/components/providers/text-to-speech/provider', () => ({
  GetDefaultSpeakerConfig: (...args: any[]) => mockGetDefaultSpeakerConfig(...args),
  GetDefaultTextToSpeechIfInvalid: (...args: any[]) =>
    mockGetDefaultTextToSpeechIfInvalid(...args),
}));

const createMetadata = (key: string, value: string): Metadata => {
  const metadata = new Metadata();
  metadata.setKey(key);
  metadata.setValue(value);
  return metadata;
};

describe('ConfigureAudioOutputProvider design integration', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('keeps only shared speaker advanced keys when switching TTS provider', () => {
    const inputParameters = [
      createMetadata('rapida.credential_id', 'cred-1'),
      createMetadata('speaker.model', 'sonic-2'),
      createMetadata('speaker.voice', 'f6f3f5f8'),
      createMetadata('speaker.language', 'en'),
      createMetadata('speaker.conjunction.boundaries', 'and<|||>or'),
      createMetadata('speaker.conjunction.break', '240'),
      createMetadata('speaker.pronunciation.dictionaries', 'medical'),
    ];

    const speakerDefaults = [createMetadata('speaker.model', 'gpt-4o-mini-tts')];
    const ttsDefaults = [createMetadata('speaker.voice', 'alloy')];
    mockGetDefaultSpeakerConfig.mockReturnValue(speakerDefaults);
    mockGetDefaultTextToSpeechIfInvalid.mockReturnValue(ttsDefaults);

    const setAudioOutputConfig = jest.fn();
    render(
      <ConfigureAudioOutputProvider
        audioOutputConfig={{ provider: 'cartesia', parameters: inputParameters }}
        setAudioOutputConfig={setAudioOutputConfig}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'change tts' }));

    const keptParams = mockGetDefaultSpeakerConfig.mock.calls[0][0] as Metadata[];
    expect(
      keptParams.map(p => `${p.getKey()}=${p.getValue()}`).sort(),
    ).toEqual(
      [
        'speaker.conjunction.boundaries=and<|||>or',
        'speaker.conjunction.break=240',
        'speaker.pronunciation.dictionaries=medical',
      ].sort(),
    );
    expect(mockGetDefaultTextToSpeechIfInvalid).toHaveBeenCalledWith(
      'openai',
      speakerDefaults,
    );
    expect(setAudioOutputConfig).toHaveBeenCalledWith({
      provider: 'openai',
      parameters: ttsDefaults,
    });
  });
});
