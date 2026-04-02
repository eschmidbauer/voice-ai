import { Globe, Settings, Microphone, VolumeUp } from '@carbon/icons-react';
import { Assistant } from '@rapidaai/react';
import { toHumanReadableDateTime } from '@/utils/date';
import { DeploymentCard } from './deployment-card';

type WebWidgetEditSection = 'experience' | 'voice-input' | 'voice-output';

interface WebWidgetDeploymentCardProps {
  assistant: Assistant;
  onEdit: () => void;
  onEditSection: (section: WebWidgetEditSection) => void;
  onDetails: () => void;
}

export function WebWidgetDeploymentCard({
  assistant,
  onEdit,
  onEditSection,
  onDetails,
}: WebWidgetDeploymentCardProps) {
  const deployment = assistant.getWebplugindeployment()!;
  return (
    <DeploymentCard
      icon={<Globe size={24} />}
      title="Web Widget"
      description="Embed a chat widget on your website"
      status="DEPLOYED"
      info={[
        { label: 'SDK', value: 'JavaScript' },
        {
          label: 'Input',
          value: `Text${deployment.getInputaudio() ? ' + Audio' : ''}`,
        },
        {
          label: 'Output',
          value: `Text${deployment.getOutputaudio() ? ' + Audio' : ''}`,
        },
        {
          label: 'Updated',
          value: toHumanReadableDateTime(deployment.getCreateddate()!),
        },
      ]}
      editMenuItems={[
        { label: 'Experience', renderIcon: Settings, onClick: () => onEditSection('experience') },
        { label: 'Voice Input', renderIcon: Microphone, onClick: () => onEditSection('voice-input') },
        { label: 'Voice Output', renderIcon: VolumeUp, onClick: () => onEditSection('voice-output') },
      ]}
      onEdit={onEdit}
      onDetails={onDetails}
    />
  );
}
