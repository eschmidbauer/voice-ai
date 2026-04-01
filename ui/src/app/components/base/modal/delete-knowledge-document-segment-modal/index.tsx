import { DeleteKnowledgeDocumentSegment } from '@rapidaai/react';
import { BaseResponse } from '@rapidaai/react';
import { KnowledgeDocumentSegment } from '@rapidaai/react';
import { ServiceError } from '@rapidaai/react';
import { Modal, ModalHeader, ModalBody, ModalFooter } from '@/app/components/carbon/modal';
import { FormLabel } from '@/app/components/form-label';
import { PrimaryButton, GhostButton } from '@/app/components/carbon/button';
import { ErrorMessage } from '@/app/components/form/error-message';
import { FieldSet } from '@/app/components/form/fieldset';
import { Textarea } from '@/app/components/form/textarea';
import { useCurrentCredential } from '@/hooks/use-credential';
import { FC, useState } from 'react';
import { connectionConfig } from '@/configs';

export const DeleteKnowledgeDocumentSegmentDialog: FC<{
  segment: KnowledgeDocumentSegment;
  onClose: () => void;
  onDelete: () => void;
}> = ({ segment, onClose, onDelete }) => {
  const { authId, token, projectId } = useCurrentCredential();
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleDelete = () => {
    if (!reason.trim()) {
      setError('Please provide a reason for deletion.');
      return;
    }
    setError(null);
    DeleteKnowledgeDocumentSegment(
      connectionConfig,
      segment.getDocumentId(),
      segment.getIndex().toString(),
      reason.trim(),
      (err: ServiceError | null, response: BaseResponse | null) => {
        if (err) {
          console.error('Error deleting segment:', err);
          setError('Failed to delete the segment. Please try again.');
        } else {
          console.log('Segment deleted successfully:', response);
          onDelete();
          onClose();
        }
      },
      {
        authorization: token,
        'x-project-id': projectId,
        'x-auth-id': authId,
      },
    );
  };

  return (
    <Modal open={true} onClose={onClose} size="sm" danger>
      <ModalHeader
        title="Are you sure you want to delete this document segment?"
        onClose={onClose}
      />
      <ModalBody hasForm>
        <FieldSet>
          <FormLabel htmlFor="delete-reason">Reason</FormLabel>
          <Textarea
            name="delete-reason"
            value={reason}
            onChange={e => setReason(e.target.value)}
            placeholder="Please provide a reason for deleting this segment"
            rows={4}
          />
        </FieldSet>
        <ErrorMessage message={error || ''} />
      </ModalBody>
      <ModalFooter danger>
        <GhostButton size="md" type="button" onClick={onClose}>
          Cancel
        </GhostButton>
        <PrimaryButton size="md" type="button" onClick={handleDelete}>
          Delete Segment
        </PrimaryButton>
      </ModalFooter>
    </Modal>
  );
};
