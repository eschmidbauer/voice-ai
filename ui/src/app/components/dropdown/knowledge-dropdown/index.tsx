import { Knowledge } from '@rapidaai/react';
import { useCredential } from '@/hooks/use-credential';
import { useKnowledgePageStore } from '@/hooks/use-knowledge-page-store';
import { Renew, Launch } from '@carbon/icons-react';
import { FC, useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast/headless';
import { Dropdown, Button } from '@carbon/react';

interface KnowledgeDropdownProps {
  className?: string;
  currentKnowledge?: string;
  onChangeKnowledge?: (k: Knowledge) => void;
}

export const KnowledgeDropdown: FC<KnowledgeDropdownProps> = props => {
  const [userId, token, projectId] = useCredential();
  const knowledgeActions = useKnowledgePageStore();
  const [, setLoading] = useState(false);

  const showLoader = () => setLoading(true);
  const hideLoader = () => setLoading(false);

  const onError = useCallback((err: string) => {
    hideLoader();
    toast.error(err);
  }, []);

  const onSuccess = useCallback((data: Knowledge[]) => {
    hideLoader();
  }, []);

  const getKnowledges = useCallback((projectId, token, userId) => {
    showLoader();
    knowledgeActions.getAllKnowledge(projectId, token, userId, onError, onSuccess);
  }, []);

  useEffect(() => {
    if (props.currentKnowledge) {
      knowledgeActions.addCriteria('id', props.currentKnowledge, 'or');
    }
    getKnowledges(projectId, token, userId);
  }, [
    projectId,
    knowledgeActions.page,
    knowledgeActions.pageSize,
    JSON.stringify(knowledgeActions.criteria),
    props.currentKnowledge,
  ]);

  const selectedItem = knowledgeActions.knowledgeBases.find(
    x => x.getId() === props.currentKnowledge,
  ) || null;

  return (
    <div>
      <p className="text-xs font-medium mb-1">Knowledge</p>
      <div className="flex">
        <div className="flex-1 [&_.cds--dropdown]:!rounded-none [&_.cds--list-box]:!rounded-none">
          <Dropdown
            id="knowledge-dropdown"
            titleText=""
            hideLabel
            label="Select knowledge"
            items={knowledgeActions.knowledgeBases}
            selectedItem={selectedItem}
            itemToString={(item: Knowledge | null) =>
              item ? `${item.getName()} [${item.getId()}]` : ''
            }
            onChange={({ selectedItem }: any) => {
              if (selectedItem && props.onChangeKnowledge) {
                props.onChangeKnowledge(selectedItem);
              }
            }}
          />
        </div>
        <Button
          hasIconOnly
          renderIcon={Renew}
          iconDescription="Refresh"
          kind="ghost"
          size="md"
          onClick={() => getKnowledges(projectId, token, userId)}
          className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
        />
        <Button
          hasIconOnly
          renderIcon={Launch}
          iconDescription="Create knowledge"
          kind="ghost"
          size="md"
          onClick={() => window.open('/knowledge/create-knowledge', '_blank')}
          className="!rounded-none !border !border-l-0 !border-gray-200 dark:!border-gray-700"
        />
      </div>
    </div>
  );
};
