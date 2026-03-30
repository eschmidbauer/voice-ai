import React, { FC, useState } from 'react';
import { CreateAssistantTelemetryProvider, Metadata } from '@rapidaai/react';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { useCurrentCredential } from '@/hooks/use-credential';
import { useRapidaStore } from '@/hooks';
import { connectionConfig } from '@/configs';
import toast from 'react-hot-toast/headless';
import {
  PrimaryButton,
  SecondaryButton,
} from '@/app/components/carbon/button';
import { Stack } from '@/app/components/carbon/form';
import { Notification } from '@/app/components/carbon/notification';
import { ButtonSet, Breadcrumb, BreadcrumbItem } from '@carbon/react';
import { InputCheckbox } from '@/app/components/form/checkbox';
import { useConfirmDialog } from '@/app/pages/assistant/actions/hooks/use-confirmation';
import { TelemetryProvider } from '@/app/components/providers/telemetry';
import {
  GetDefaultTelemetryIfInvalid,
  ValidateTelemetry,
} from '@/app/components/providers/telemetry/provider';
import { TELEMETRY_PROVIDER } from '@/providers';

export const CreateAssistantTelemetry: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigator = useGlobalNavigation();
  const { authId, token, projectId } = useCurrentCredential();
  const { loading, showLoader, hideLoader } = useRapidaStore();
  const { showDialog, ConfirmDialogComponent } = useConfirmDialog({});

  const defaultProvider = TELEMETRY_PROVIDER[0]?.code || 'otlp_http';
  const [provider, setProvider] = useState(defaultProvider);
  const [parameters, setParameters] = useState<Metadata[]>(() =>
    GetDefaultTelemetryIfInvalid(defaultProvider, []),
  );
  const [enabled, setEnabled] = useState(true);
  const [errorMessage, setErrorMessage] = useState('');

  const onChangeProvider = (providerCode: string) => {
    setProvider(providerCode);
    const credentialOnly = parameters.filter(
      p => p.getKey() === 'rapida.credential_id',
    );
    setParameters(GetDefaultTelemetryIfInvalid(providerCode, credentialOnly));
  };

  const onSubmit = () => {
    setErrorMessage('');
    const validationError = ValidateTelemetry(provider, parameters);
    if (validationError) {
      setErrorMessage(validationError);
      return;
    }

    showLoader();
    CreateAssistantTelemetryProvider(
      connectionConfig,
      assistantId,
      provider,
      enabled,
      parameters,
      (err, response) => {
        hideLoader();
        if (err) {
          setErrorMessage('Unable to create telemetry provider. Please try again.');
          return;
        }
        if (response?.getSuccess()) {
          toast.success('Telemetry provider created successfully');
          navigator.goToAssistantTelemetry(assistantId);
          return;
        }
        setErrorMessage(
          response?.getError()?.getHumanmessage() ||
            'Unable to create telemetry provider. Please try again.',
        );
      },
      {
        'x-auth-id': authId,
        authorization: token,
        'x-project-id': projectId,
      },
    );
  };

  return (
    <>
      <ConfirmDialogComponent />
      <div className="flex flex-col flex-1 min-h-0">
        {/* Header */}
        <div className="px-8 pt-6 pb-4 border-b border-gray-200 dark:border-gray-800 shrink-0">
          <Breadcrumb noTrailingSlash className="mb-2">
            <BreadcrumbItem href={`/deployment/assistant/${assistantId}/configure-telemetry`}>
              Telemetry
            </BreadcrumbItem>
          </Breadcrumb>
          <h1 className="text-xl font-light tracking-tight">Create Telemetry Provider</h1>
        </div>

        {/* Form */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          <div className="px-8 pt-6 pb-8 max-w-4xl">
            <Stack gap={6}>
              <TelemetryProvider
                provider={provider}
                onChangeProvider={onChangeProvider}
                parameters={parameters}
                onChangeParameter={setParameters}
              />
              <InputCheckbox
                checked={enabled}
                onChange={e => setEnabled(e.target.checked)}
              >
                Enable this telemetry provider
              </InputCheckbox>
              {errorMessage && (
                <Notification kind="error" title="Error" subtitle={errorMessage} />
              )}
            </Stack>
          </div>
        </div>

        {/* Footer */}
        <ButtonSet className="!w-full [&>button]:!flex-1 [&>button]:!max-w-none">
          <SecondaryButton size="lg" onClick={() => showDialog(navigator.goBack)}>
            Cancel
          </SecondaryButton>
          <PrimaryButton size="lg" isLoading={loading} onClick={onSubmit}>
            Save telemetry
          </PrimaryButton>
        </ButtonSet>
      </div>
    </>
  );
};
