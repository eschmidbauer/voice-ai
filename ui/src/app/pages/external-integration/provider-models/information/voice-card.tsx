import { TertiaryButton, GhostButton } from '@/app/components/carbon/button';
import { Check, Copy, Pause, Play } from 'lucide-react';
import { FC, useRef, useState } from 'react';

export const VoiceCard: FC<{
  title: string;
  voiceId: string;
  description?: string | null;
  previewUrl?: string;
  languages: any[];
  persona: string[];
  features: any[];
}> = ({
  title,
  voiceId,
  description,
  previewUrl,
  languages,
  persona,
  features,
}) => {
  const [copied, setCopied] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);
  const audioRef = useRef<HTMLAudioElement | null>(null);

  const handleCopy = () => {
    navigator.clipboard.writeText(voiceId);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const togglePlayback = () => {
    if (!audioRef.current) return;
    if (isPlaying) {
      audioRef.current.pause();
    } else {
      audioRef.current.play();
    }
    setIsPlaying(!isPlaying);
  };

  return (
    <div className="rounded-[2px] border bg-white dark:bg-gray-950 col-span-1 flex flex-col justify-between">
      <div className="flex justify-between w-full border-b dark:border-gray-900">
        <div className="py-1.5 pl-4">
          <h2 className="text-base/6 font-medium capitalize">{title}</h2>
        </div>
        {previewUrl && <audio ref={audioRef} src={previewUrl} />}
        {previewUrl && (
          <TertiaryButton
            size="md"
            onClick={togglePlayback}
            className="p-1 px-2.5 bg-blue-600/10 dark:bg-blue-600/10"
          >
            {isPlaying ? (
              <Pause className="w-4 h-4 shrink-0" />
            ) : (
              <Play className="w-4 h-4 shrink-0" />
            )}
          </TertiaryButton>
        )}
      </div>
      <div className="py-2 px-4 flex justify-between">
        <p className="text-sm/6 font-medium dark:text-gray-600 text-gray-500">
          Languages
        </p>
        <div className="flex space-x-2">
          {languages
            .filter(p => p !== undefined)
            .map((p, idx) => (
              <span key={idx} className="text-sm">
                {p}
              </span>
            ))}
        </div>
      </div>
      <div className="border-t dark:border-gray-900">
        <div className="py-2 px-4 flex justify-between">
          <p className="text-sm/6 font-medium dark:text-gray-600 text-gray-500">
            Persona
          </p>
          <div className="flex space-x-2">
            {persona
              .filter(p => p !== undefined)
              .map((p, idx) => (
                <span key={idx} className="capitalize text-sm">
                  {p}
                </span>
              ))}
          </div>
        </div>

        <div className="border-t py-2 dark:border-gray-900">
          <p className="text-sm/6 px-4 line-clamp-2 dark:text-gray-600 text-gray-400">
            {description ? description : 'Not available'}
          </p>
        </div>
      </div>
      <div className="border-t dark:border-gray-900">
        <div className="py-2 pl-4">
          <p className="text-sm/6 font-medium dark:text-gray-600 text-gray-500">
            Features and usecase
          </p>
        </div>
        <div className="flex flex-wrap gap-1 px-4 pb-3">
          {features
            .filter(p => p !== undefined)
            .map((x, idx) => {
              return (
                <div
                  key={idx}
                  className="flex items-center gap-2 border rounded-full px-3 py-1 bg-yellow-600/5 text-yellow-600 border-yellow-600/20"
                >
                  <span className=" font-medium text-xs/6 capitalize">{x}</span>
                </div>
              );
            })}
        </div>
      </div>
      <div className="flex items-center justify-between rounded-b-[2px] border-t dark:border-gray-900">
        <div className="py-2 pl-4">
          <p className="text-sm/6 font-medium dark:text-gray-600 text-gray-500">
            Voice ID
          </p>
          <p className="text-xs/6 font-mono">{voiceId}</p>
        </div>
        <GhostButton size="md" onClick={handleCopy} title="Copy Voice ID">
          {copied ? (
            <Check className="text-green-400 w-4 h-4" strokeWidth={1.5} />
          ) : (
            <Copy className="text-slate-400 w-4 h-4" strokeWidth={1.5} />
          )}
        </GhostButton>
      </div>
    </div>
  );
};
