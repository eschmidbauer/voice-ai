import { Phone, PhoneSettings, Settings, Microphone, VolumeUp } from '@carbon/icons-react';
import { Assistant } from '@rapidaai/react';
import { toHumanReadableDateTime } from '@/utils/date';
import { DeploymentCard } from './deployment-card';

type PhoneEditSection = 'telephony' | 'experience' | 'voice-input' | 'voice-output';

interface PhoneDeploymentCardProps {
  assistant: Assistant;
  onEdit: () => void;
  onEditSection: (section: PhoneEditSection) => void;
  onPreview: () => void;
  onDetails: () => void;
}

export function PhoneDeploymentCard({
  assistant,
  onEdit,
  onEditSection,
  onPreview,
  onDetails,
}: PhoneDeploymentCardProps) {
  const deployment = assistant.getPhonedeployment()!;
  return (
    <DeploymentCard
      icon={<Phone size={24} />}
      title="Phone Call"
      description="Deploy on inbound or outbound calls"
      status="DEPLOYED"
      info={[
        {
          label: 'Telephony',
          value: deployment.getPhoneprovidername() || '—',
        },
        { label: 'Input', value: 'Audio' },
        { label: 'Output', value: 'Audio' },
        {
          label: 'Updated',
          value: toHumanReadableDateTime(deployment.getCreateddate()!),
        },
      ]}
      editMenuItems={[
        { label: 'Telephony', renderIcon: PhoneSettings, onClick: () => onEditSection('telephony') },
        { label: 'Experience', renderIcon: Settings, onClick: () => onEditSection('experience') },
        { label: 'Voice Input', renderIcon: Microphone, onClick: () => onEditSection('voice-input') },
        { label: 'Voice Output', renderIcon: VolumeUp, onClick: () => onEditSection('voice-output') },
      ]}
      onEdit={onEdit}
      onPreview={onPreview}
      onDetails={onDetails}
    />
  );
}
