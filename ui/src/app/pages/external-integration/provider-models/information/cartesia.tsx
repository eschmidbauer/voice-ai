import { useEffect, useState } from 'react';
import { SearchIconInput } from '@/app/components/form/input/IconInput';
import { Helmet } from '@/app/components/helmet';
import { BluredWrapper } from '@/app/components/wrapper/blured-wrapper';
import { PageHeaderBlock } from '@/app/components/blocks/page-header-block';
import { PageTitleBlock } from '@/app/components/blocks/page-title-block';
import { TEXT_TO_SPEECH, CARTESIA_VOICE } from '@/providers';
import { PaginationButtonBlock } from '@/app/components/blocks/pagination-button-block';
import { cn } from '@/utils';
import { CreateProviderCredentialDialog } from '@/app/components/base/modal/create-provider-credential-modal';
import { ViewProviderCredentialDialog } from '@/app/components/base/modal/view-provider-credential-modal';
import { useAllProviderCredentials } from '@/hooks/use-model';
import { Check, Plus } from 'lucide-react';
import { Tooltip } from '@/app/components/tooltip';
import { PrimaryButton, GhostButton } from '@/app/components/carbon/button';
import { VoiceCard } from '@/app/pages/external-integration/provider-models/information/voice-card';
import { useLocation } from 'react-router-dom';

/**
 *
 * @returns
 */
export function CartesiaModelInformationPage() {
  const [provider] = useState(TEXT_TO_SPEECH('cartesia'));
  const [filteredVoices, setFilteredVoices] = useState(CARTESIA_VOICE());
  const { providerCredentials } = useAllProviderCredentials();
  const [createProviderModalOpen, setCreateProviderModalOpen] = useState(false);
  const [viewProviderModalOpen, setViewProviderModalOpen] = useState(false);
  const [connected, setConnected] = useState(false);
  const location = useLocation(); // Get the current URL including query params
  useEffect(() => {
    // Check for voice_id query parameter in the URL
    const params = new URLSearchParams(location.search);
    const voiceId = params.get('query');
    if (voiceId) {
      searchVoice(voiceId);
    }
  }, [location.search]);

  const searchVoice = (v: string) => {
    const voices = CARTESIA_VOICE();
    if (v.length > 0) {
      setFilteredVoices(
        voices.filter(
          voice =>
            voice.name.toLowerCase().includes(v.toLowerCase()) ||
            voice.id.toLowerCase().includes(v.toLowerCase()) ||
            voice.language?.toLowerCase().includes(v.toLowerCase()),
        ),
      );
      return;
    }
    setFilteredVoices(voices);
  };

  useEffect(() => {
    let isFoundCredential = providerCredentials.find(
      x => x.getProvider() === provider?.code,
    );
    if (isFoundCredential) setConnected(true);
  }, [JSON.stringify(provider), JSON.stringify(providerCredentials)]);

  return (
    <div className="flex flex-1 overflow-auto flex-col">
      <CreateProviderCredentialDialog
        modalOpen={createProviderModalOpen}
        setModalOpen={setCreateProviderModalOpen}
        currentProvider={provider?.code}
      ></CreateProviderCredentialDialog>
      <ViewProviderCredentialDialog
        modalOpen={viewProviderModalOpen}
        setModalOpen={setViewProviderModalOpen}
        currentProvider={provider!}
        onSetupCredential={() => {
          setViewProviderModalOpen(!viewProviderModalOpen);
          setCreateProviderModalOpen(!createProviderModalOpen);
        }}
      />
      <Helmet title="Provider information" />
      <PageHeaderBlock>
        <div className="flex items-center gap-3 py-4">
          <div className="rounded-[2px] flex items-center justify-center shrink-0 h-16 w-16 dark:bg-gray-600 border dark:border-gray-700">
            <img
              src={provider?.image}
              alt={provider?.name}
              className="rounded-[2px]"
            />
          </div>
          <PageTitleBlock className="">
            <div className="flex items-center space-x-1">
              <span className="capitalize">{provider?.name}</span>
              <span className="inline-flex items-center">
                <Tooltip
                  icon={
                    <Check
                      className={cn(
                        connected
                          ? 'bg-blue-500 text-white'
                          : 'bg-gray-500 text-white',
                        'w-3.5 h-3.5 rounded-full p-0.5 flex-shrink-0',
                      )}
                    />
                  }
                >
                  <span className="text-gray-600">Connection Status</span>
                </Tooltip>
              </span>
            </div>
            <p className="text-sm/6 text-muted">{provider?.description}</p>
          </PageTitleBlock>
        </div>
      </PageHeaderBlock>
      <BluredWrapper className="sticky top-0">
        <SearchIconInput
          className="bg-light-background"
          onChange={t => {
            searchVoice(t.target.value);
          }}
        />
        <PaginationButtonBlock className="border-l divide-x">
          <GhostButton
            size="md"
            onClick={() => {
              setViewProviderModalOpen(true);
            }}
          >
            View credential
          </GhostButton>
          <PrimaryButton
            size="md"
            onClick={() => {
              setCreateProviderModalOpen(true);
            }}
          >
            Add new credential
            <Plus strokeWidth={1.5} className="ml-1.5 h-4 w-4" />
          </PrimaryButton>
        </PaginationButtonBlock>
      </BluredWrapper>
      <div className="grid gap-3 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 p-4">
        {filteredVoices.map((voice, idx) => {
          return (
            <VoiceCard
              title={voice.name}
              voiceId={voice.id}
              key={idx}
              description={voice.description}
              languages={[voice?.language]}
              features={[voice.mode]}
              persona={[]}
            />
          );
        })}
      </div>
    </div>
  );
}
