import type { DeleteSessionConfirmModalProps } from '@/types';
import UnifiedModal from '../../others/UnifiedModal';

const DeleteSessionConfirmModal = ({
  isOpen,
  sessionId,
  sessionName,
  onClose,
  onConfirm
}: DeleteSessionConfirmModalProps) => {
  if (!isOpen) return null;

  return (
    <UnifiedModal
      title="确认删除"
      message={`确定要删除对话 "${sessionName}" 吗？此操作无法撤销。`}
      buttons={[
        { text: '确定', onClick: () => onConfirm(sessionId), className: 'bg-theme-green' },
        { text: '取消', onClick: onClose, className: 'bg-theme-gray3' }
      ]}
    />
  );
};

export default DeleteSessionConfirmModal;
