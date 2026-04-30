import { useEffect, useRef, useState } from 'react';
import './App.css';
import { useDispatch, useSelector } from 'react-redux';
import type { RootState } from './store/store';
import {
  addLog,
  setLogs,
  setCopied,
  setMainRunning,
  setProgress,
  setUpdateStatus,
  setCheckingUpdate,
  setUpdating,
  setVersion,
  setLaunching,
  setLaunchPhase,
  resetProgress,
  addWebviewTab,
} from './store/launcher';
import { useTheme } from './context/ThemeContext';
import {
  CheckUpdate,
  GetLogs,
  GetVersion,
  IsMainProgramRunning,
  IsProjectDeployed,
  PrepareEnvironment,
  DownloadLaunch,
  LoadConfig,
  PerformUpdate,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime';
import GitManager from './components/GitManager';
import WebviewTab from './components/WebviewTab';

function App() {
  const dispatch = useDispatch();
  const {
    logs,
    version,
    updateStatus,
    checkingUpdate,
    updating,
    progress,
    copied,
    mainRunning,
    launching,
    launchPhase,
    webviewTabs,
  } = useSelector((state: RootState) => state.launcherSlice);

  const { theme } = useTheme();
  const logRef = useRef<HTMLDivElement>(null);
  const [tab, setTab] = useState<'launcher' | 'git'>('launcher');
  const [deployed, setDeployed] = useState<boolean>(false);
  const [preparing, setPreparing] = useState(false);

  const refreshStatus = async () => {
    try {
      const v = await GetVersion();
      dispatch(setVersion(v));
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    LoadConfig().then(() => {
      IsProjectDeployed().then((d: boolean) => {
        setDeployed(d);
        if (!d) {
          dispatch(setLogs([
            '初次部署项目，请点击「下载项目」按钮\n',
          ]));
        }
        refreshStatus();
      });
      IsMainProgramRunning().then((running: boolean) => dispatch(setMainRunning(running)));
    });

    const offLog = EventsOn('log', (data: string) => {
      dispatch(addLog(data));
    });

    const offProgress = EventsOn('progress', (p: number) => {
      dispatch(setProgress(p));
    });

    const offMainState = EventsOn('main-program-state', (running: boolean) => {
      dispatch(setMainRunning(running));
      if (!running) {
        dispatch(setLaunching(false));
        dispatch(setLaunchPhase(''));
      }
    });

    const offWebview = EventsOn('open-webview-tab', (data: { title: string; url: string }) => {
      dispatch(addWebviewTab({
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
        title: data.title,
        url: data.url,
      }));
    });

    return () => {
      offLog?.();
      offProgress?.();
      offMainState?.();
      offWebview?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [logs]);

  const handleCheckUpdate = async () => {
    if (checkingUpdate) return;
    dispatch(setCheckingUpdate(true));
    try {
      const status = await CheckUpdate();
      dispatch(setUpdateStatus(status));
    } catch {
      dispatch(setUpdateStatus(null));
    } finally {
      dispatch(setCheckingUpdate(false));
    }
  };

  const handleUpdate = async () => {
    dispatch(setUpdating(true));
    try {
      await PerformUpdate();
      setDeployed(true);
      await refreshStatus();
      dispatch(setUpdateStatus(null));
      dispatch(resetProgress());
    } catch {
      dispatch(resetProgress());
    } finally {
      dispatch(setUpdating(false));
    }
  };

  const handlePrepareEnvironment = async () => {
    setPreparing(true);
    try {
      await PrepareEnvironment();
    } catch (err: any) {
      const msg = err?.message || String(err);
      dispatch(addLog(`环境准备失败: ${msg}\n`));
    } finally {
      setPreparing(false);
    }
  };

  const handleDownloadLaunch = async () => {
    dispatch(setLaunching(true));
    dispatch(setLaunchPhase('下载依赖并启动...'));
    try {
      await DownloadLaunch();
    } catch (err: any) {
      const msg = err?.message || String(err);
      dispatch(addLog(`启动失败: ${msg}\n`));
      dispatch(setLaunching(false));
      dispatch(setLaunchPhase(''));
    }
  };

  const handleCopyLogs = async () => {
    const text = await GetLogs();
    await navigator.clipboard.writeText(text);
    dispatch(setCopied(true));
    setTimeout(() => dispatch(setCopied(false)), 1500);
  };

  const getUpdateButtonText = () => {
    if (checkingUpdate) return '检查中...';
    if (updating) return '更新中...';
    if (!deployed) return '下载项目';
    if (updateStatus?.has_update) return '下载更新';
    if (updateStatus !== null) return '已是最新';
    return '检查更新';
  };

  const handleUpdateButtonClick = () => {
    if (!deployed) {
      handleUpdate();
      return;
    }
    if (updateStatus?.has_update) {
      handleUpdate();
    } else {
      handleCheckUpdate();
    }
  };

  return (
    <div className="app" style={{ background: theme.black, color: theme.white }}>
      <header className="header">
        <h1>青烛启动器</h1>
        <div className="header-right">
          <div className="tabs">
            <button
              className={`tab-btn ${tab === 'launcher' ? 'active' : ''}`}
              onClick={() => setTab('launcher')}
            >
              启动器
            </button>
            <button
              className={`tab-btn ${tab === 'git' ? 'active' : ''}`}
              onClick={() => setTab('git')}
              disabled={!deployed}
            >
              Git管理
            </button>
          </div>
          <div className="meta">
            <span className="version">本地版本: {version || '-'}</span>
          </div>
        </div>
      </header>

      <main className="main">
        {tab === 'launcher' ? (
          <>
            <div className="toolbar">
              <button
                className="btn warn"
                onClick={handleUpdateButtonClick}
                disabled={checkingUpdate || updating || launching || preparing}
              >
                {getUpdateButtonText()}
              </button>
              <button
                className="btn"
                onClick={handlePrepareEnvironment}
                disabled={preparing || launching || updating}
              >
                {preparing ? '准备中...' : '准备环境'}
              </button>
              <button
                className="btn primary"
                onClick={handleDownloadLaunch}
                disabled={mainRunning || launching || preparing || !deployed}
                title={mainRunning ? '主程序正在运行中' : !deployed ? '请先下载项目' : ''}
              >
                {mainRunning ? '运行中' : launching ? '下载启动中...' : '下载启动'}
              </button>
              <button
                className={`btn ${copied ? 'success' : ''}`}
                onClick={handleCopyLogs}
                disabled={copied}
              >
                {copied ? '复制成功' : '复制日志'}
              </button>
            </div>

            {launching && (
              <div className="launch-phase" style={{ color: theme.accent }}>
                {launchPhase}
              </div>
            )}

            {progress > 0 && progress < 100 && (
              <div className="progress-bar">
                <div className="progress-fill" style={{ width: `${progress}%` }} />
                <span className="progress-text">{progress}%</span>
              </div>
            )}

            <div className="log-box" ref={logRef}>
              {logs.map((line, idx) => (
                <div key={idx} className="log-line">
                  <span className="log-prefix">{'>'}</span>
                  <span className="log-content">{line.replace(/\n$/, '')}</span>
                </div>
              ))}
            </div>
          </>
        ) : (
          <GitManager />
        )}

        {webviewTabs.length > 0 && (
          <div className="webview-tabs-panel">
            {webviewTabs.map((t) => (
              <WebviewTab key={t.id} id={t.id} title={t.title} url={t.url} />
            ))}
          </div>
        )}
      </main>
    </div>
  );
}

export default App;
