import { ModalProps } from '@/app/components/base/modal';
import {
  CarbonModalProps,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from '@/app/components/carbon/modal';
import {
  PrimaryButton,
  SecondaryButton,
} from '@/app/components/carbon/button';
import { ErrorMessage } from '@/app/components/form/error-message';
import { ReactNode } from 'react';

type EditSection = 'experience' | 'voice-input' | 'voice-output';

interface DeploymentEditSectionModalProps extends ModalProps {
  section: EditSection;
  label?: string;
  size?: CarbonModalProps['size'];
  errorMessage?: string;
  isSaving?: boolean;
  onSave: () => void;
  children: ReactNode;
}

const sectionToTitle = (section: EditSection) => {
  if (section === 'experience') return 'General Experience';
  if (section === 'voice-input') return 'Voice Input';
  return 'Voice Output';
};

export function DeploymentEditSectionModal(
  props: DeploymentEditSectionModalProps,
) {
  const title = sectionToTitle(props.section);
  return (
    <Modal
      open={props.modalOpen}
      onClose={() => props.setModalOpen(false)}
      size={props.size || 'lg'}
      selectorPrimaryFocus="#deployment-voice-input-toggle"
      preventCloseOnClickOutside
    >
      <ModalHeader
        label={props.label || 'Deployment'}
        title={title}
        onClose={() => props.setModalOpen(false)}
      />
      <ModalBody hasForm className="[mask-image:none]">
        <ErrorMessage message={props.errorMessage} />
        {props.children}
      </ModalBody>
      <ModalFooter>
        <SecondaryButton
          size="lg"
          onClick={() => props.setModalOpen(false)}
          disabled={props.isSaving}
        >
          Cancel
        </SecondaryButton>
        <PrimaryButton
          size="lg"
          onClick={props.onSave}
          isLoading={props.isSaving}
          disabled={props.isSaving}
        >
          Save
        </PrimaryButton>
      </ModalFooter>
    </Modal>
  );
}

/** @deprecated Use DeploymentEditSectionModal instead */
export const AssistantDebuggerEditSectionModal = DeploymentEditSectionModal;
