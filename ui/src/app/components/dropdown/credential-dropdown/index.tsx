import { VaultCredential } from '@rapidaai/react';
import { Renew, Add } from '@carbon/icons-react';
import { FC, useEffect, useState } from 'react';
import { CreateProviderCredentialDialog } from '@/app/components/base/modal/create-provider-credential-modal';
import { useAllProviderCredentials } from '@/hooks/use-model';
import { useProviderContext } from '@/context/provider-context';
import { allProvider } from '@/providers';
import { Button } from '@carbon/react';
import { Dropdown } from '@/app/components/carbon/dropdown';

interface CredentialDropdownProps {
  className?: string;
  provider?: string;
  currentCredential?: string;
  onChangeCredential: (credential: VaultCredential) => void;
}

export const CredentialDropdown: FC<CredentialDropdownProps> = props => {
  const { providerCredentials } = useAllProviderCredentials();
  const ctx = useProviderContext();
  const [createProviderModalOpen, setCreateProviderModalOpen] = useState(false);
  const [currentProviderCredentials, setCurrentProviderCredentials] = useState<
    Array<VaultCredential>
  >([]);

  useEffect(() => {
    setCurrentProviderCredentials(
      providerCredentials.filter(y => y.getProvider() === props.provider),
    );
  }, [providerCredentials, props.provider]);

  const selectedItem = currentProviderCredentials.find(
    x => x.getId() === props.currentCredential,
  ) || null;

  const getProviderInfo = (code: string) =>
    allProvider().find(x => x.code === code);

  return (
    <>
      <CreateProviderCredentialDialog
        modalOpen={createProviderModalOpen}
        setModalOpen={setCreateProviderModalOpen}
        currentProvider={props.provider}
      />
      <div>
        <div className="flex items-end">
          <div className="flex-1 min-w-0">
            <Dropdown
              id="credential-dropdown"
              titleText="Credential"
              hideLabel={false}
              label="Select credential"
              items={currentProviderCredentials}
              selectedItem={selectedItem}
              itemToString={(item: VaultCredential | null) => {
                if (!item) return '';
                const provider = getProviderInfo(item.getProvider());
                return provider
                  ? `${provider.name} / ${item.getName()}`
                  : item.getName();
              }}
              onChange={({ selectedItem }: any) => {
                if (selectedItem) props.onChangeCredential(selectedItem);
              }}
            />
          </div>
          <Button
            hasIconOnly
            renderIcon={Renew}
            iconDescription="Refresh credentials"
            kind="ghost"
            size="md"
            onClick={() => ctx.reloadProviderCredentials()}
            className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
          />
          <Button
            hasIconOnly
            renderIcon={Add}
            iconDescription="Create credential"
            kind="ghost"
            size="md"
            onClick={() => setCreateProviderModalOpen(true)}
            className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
          />
        </div>
      </div>
    </>
  );
};
