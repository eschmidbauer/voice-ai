import { Endpoint } from '@rapidaai/react';
import { useEndpointPageStore } from '@/hooks';
import { useCredential } from '@/hooks/use-credential';
import { Renew, Launch } from '@carbon/icons-react';
import { FC, useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast/headless';
import { Dropdown, Button } from '@carbon/react';

interface EndpointDropdownProps {
  className?: string;
  currentEndpoint?: string;
  onChangeEndpoint: (endpoint: Endpoint) => void;
}

export const EndpointDropdown: FC<EndpointDropdownProps> = props => {
  const [userId, token, projectId] = useCredential();
  const endpointActions = useEndpointPageStore();
  const [, setLoading] = useState(false);

  const showLoader = () => setLoading(true);
  const hideLoader = () => setLoading(false);

  const onError = useCallback((err: string) => {
    hideLoader();
    toast.error(err);
  }, []);

  const onSuccess = useCallback((data: Endpoint[]) => {
    hideLoader();
  }, []);

  const getEndpoints = useCallback((projectId, token, userId) => {
    showLoader();
    endpointActions.onGetAllEndpoint(projectId, token, userId, onError, onSuccess);
  }, []);

  useEffect(() => {
    if (props.currentEndpoint) {
      endpointActions.addCriteria('id', props.currentEndpoint, 'or');
    }
    getEndpoints(projectId, token, userId);
  }, [
    projectId,
    endpointActions.page,
    endpointActions.pageSize,
    JSON.stringify(endpointActions.criteria),
    props.currentEndpoint,
  ]);

  const selectedItem = endpointActions.endpoints.find(
    x => x.getId() === props.currentEndpoint,
  ) || null;

  return (
    <div>
      <p className="text-xs font-medium mb-1">Endpoint</p>
      <div className="flex">
        <div className="flex-1 [&_.cds--dropdown]:!rounded-none [&_.cds--list-box]:!rounded-none">
          <Dropdown
            id="endpoint-dropdown"
            titleText=""
            hideLabel
            label="Select endpoint"
            items={endpointActions.endpoints}
            selectedItem={selectedItem}
            itemToString={(item: Endpoint | null) =>
              item ? `${item.getName()} [${item.getId()}]` : ''
            }
            onChange={({ selectedItem }: any) => {
              if (selectedItem) props.onChangeEndpoint(selectedItem);
            }}
          />
        </div>
        <Button
          hasIconOnly
          renderIcon={Renew}
          iconDescription="Refresh"
          kind="ghost"
          size="md"
          onClick={() => getEndpoints(projectId, token, userId)}
          className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
        />
        <Button
          hasIconOnly
          renderIcon={Launch}
          iconDescription="Create endpoint"
          kind="ghost"
          size="md"
          onClick={() => window.open('/deployment/endpoint/create-endpoint', '_blank')}
          className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
        />
      </div>
    </div>
  );
};
