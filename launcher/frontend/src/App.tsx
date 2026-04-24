import { useEffect, useRef, useState } from 'react';
import './App.css';
import { useDispatch, useSelector } from 'react-redux';
import type { RootState } from './store/store';
import {
  addLog,
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
  CheckPythonVersion,
  CheckUpdate,
  GetLogs,
  GetVersion,
  IsMainProgramRunning,
  IsProjectDeployed,
  LaunchMainProgram,
  LoadConfig,
  PerformUpdate,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime';
import GitManager from './components/GitManager';
import WebviewTab from './components/WebviewTab';

interface PythonVersionCheck {
  found: boolean;
  version: string;
  ok: boolean;
  message: string;
}

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
  const [pythonCheck, setPythonCheck] = useState<PythonVersionCheck | null>(null);
  const [showPythonAlert, setShowPythonAlert] = useState(false);

  const refreshStatus = async () => {
    try {
      const v = await GetVersion();
      dispatch(setVersion(v));
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    LoadConfig().then((cfg) => {
      // 检测是否需要检查 Python 3.13.9
      if (cfg?.Python?.require_3_13_9) {
        CheckPythonVersion().then((check) => {
          setPythonCheck(check);
          if (!check.ok) {
            setShowPythonAlert(true);
          }
        });
      }
      IsProjectDeployed().then((d: boolean) => {
        setDeployed(d);
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

  const handleLaunch = async () => {
    dispatch(setLaunching(true));
    dispatch(setLaunchPhase('准备启动...'));
    try {
      await LaunchMainProgram();
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

  const handleOpenInstallGuide = () => {
    dispatch(addWebviewTab({
      id: 'python-install-guide',
      title: 'Python 安装教程',
      url: 'https://denghuominghui.cn',
    }));
  };

  const remoteMsg = updateStatus?.remote_commit?.message ?? '';
  const remoteSha = updateStatus?.remote_commit?.sha ?? '';
  const localSha = updateStatus?.local_commit?.sha ?? '';

  const getUpdateButtonText = () => {
    if (checkingUpdate) return '检查中...';
    if (updating) return '更新中...';
    if (!deployed) return '下载项目';
    if (updateStatus?.has_update) return '下载更新';
    if (updateStatus !== null) return '当前已是最新更新';
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
            {showPythonAlert && pythonCheck && (
              <div className="python-alert" style={{ background: theme.dark, borderColor: theme.warn }}>
                <span className="python-alert-msg" style={{ color: theme.warn }}>
                  {pythonCheck.message}
                </span>
                <button className="btn warn" onClick={handleOpenInstallGuide}>
                  查看安装教程
                </button>
              </div>
            )}

            <div className="toolbar">
              <button
                className="btn warn"
                onClick={handleUpdateButtonClick}
                disabled={checkingUpdate || updating || launching}
              >
                {getUpdateButtonText()}
              </button>
              <button
                className="btn primary"
                onClick={handleLaunch}
                disabled={mainRunning || launching || !deployed}
                title={mainRunning ? '主程序正在运行中' : !deployed ? '请先下载项目' : ''}
              >
                {mainRunning ? '运行中' : launching ? '启动中...' : '启动程序'}
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

            {updateStatus !== null && (
              <div className="update-info">
                <div className="commit-row">
                  <span className="commit-label">远程提交:</span>
                  <span className="commit-sha">{remoteSha.slice(0, 7)}</span>
                </div>
                <div className="commit-msg">{remoteMsg}</div>
                {localSha && (
                  <div className="commit-row">
                    <span className="commit-label">本地提交:</span>
                    <span className="commit-sha">{localSha.slice(0, 7)}</span>
                  </div>
                )}
              </div>
            )}

            {progress > 0 && progress < 100 && (
              <div className="progress-bar">
                <div className="progress-fill" style={{ width: `${progress}%` }} />
                <span className="progress-text">{progress}%</span>
              </div>
            )}

            <div className="log-box" ref={logRef}>
              {logs.length === 0 && (
                <div className="log-placeholder">等待日志输出...</div>
              )}
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
