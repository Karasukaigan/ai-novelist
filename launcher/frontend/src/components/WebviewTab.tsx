import { useDispatch } from 'react-redux';
import { removeWebviewTab } from '../store/launcher';

interface WebviewTabProps {
  id: string;
  title: string;
  url: string;
}

export default function WebviewTab({ id, title, url }: WebviewTabProps) {
  const dispatch = useDispatch();

  return (
    <div className="webview-tab">
      <div className="webview-header">
        <span className="webview-title">{title}</span>
        <button
          className="webview-close"
          onClick={() => dispatch(removeWebviewTab(id))}
        >
          ×
        </button>
      </div>
      <iframe
        className="webview-frame"
        src={url}
        title={title}
        sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
      />
    </div>
  );
}
