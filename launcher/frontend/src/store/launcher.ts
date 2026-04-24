import { createSlice, type Draft, type PayloadAction } from '@reduxjs/toolkit';
import type { updater } from '../../wailsjs/go/models';

export interface WebviewTab {
  id: string;
  title: string;
  url: string;
}

export interface LauncherState {
  logs: string[];
  version: string;
  updateStatus: updater.UpdateStatus | null;
  checkingUpdate: boolean;
  updating: boolean;
  progress: number;
  copied: boolean;
  mainRunning: boolean;
  launching: boolean;
  launchPhase: string;
  webviewTabs: WebviewTab[];
}

const initialState: LauncherState = {
  logs: [],
  version: '',
  updateStatus: null,
  checkingUpdate: false,
  updating: false,
  progress: 0,
  copied: false,
  mainRunning: false,
  launching: false,
  launchPhase: '',
  webviewTabs: [],
};

export const launcherSlice = createSlice({
  name: 'launcherSlice',
  initialState,
  reducers: {
    addLog: (state: Draft<LauncherState>, action: PayloadAction<string>) => {
      state.logs.push(action.payload);
    },
    setLogs: (state: Draft<LauncherState>, action: PayloadAction<string[]>) => {
      state.logs = action.payload;
    },
    setVersion: (state: Draft<LauncherState>, action: PayloadAction<string>) => {
      state.version = action.payload;
    },
    setUpdateStatus: (state: Draft<LauncherState>, action: PayloadAction<updater.UpdateStatus | null>) => {
      state.updateStatus = action.payload;
    },
    setCheckingUpdate: (state: Draft<LauncherState>, action: PayloadAction<boolean>) => {
      state.checkingUpdate = action.payload;
    },
    setUpdating: (state: Draft<LauncherState>, action: PayloadAction<boolean>) => {
      state.updating = action.payload;
    },
    setProgress: (state: Draft<LauncherState>, action: PayloadAction<number>) => {
      state.progress = action.payload;
    },
    setCopied: (state: Draft<LauncherState>, action: PayloadAction<boolean>) => {
      state.copied = action.payload;
    },
    setMainRunning: (state: Draft<LauncherState>, action: PayloadAction<boolean>) => {
      state.mainRunning = action.payload;
    },
    setLaunching: (state: Draft<LauncherState>, action: PayloadAction<boolean>) => {
      state.launching = action.payload;
    },
    setLaunchPhase: (state: Draft<LauncherState>, action: PayloadAction<string>) => {
      state.launchPhase = action.payload;
    },
    resetProgress: (state: Draft<LauncherState>) => {
      state.progress = 0;
    },
    addWebviewTab: (state: Draft<LauncherState>, action: PayloadAction<WebviewTab>) => {
      const exists = state.webviewTabs.find((t) => t.id === action.payload.id);
      if (!exists) {
        state.webviewTabs.push(action.payload);
      }
    },
    removeWebviewTab: (state: Draft<LauncherState>, action: PayloadAction<string>) => {
      state.webviewTabs = state.webviewTabs.filter((t) => t.id !== action.payload);
    },
  },
});

export const {
  addLog,
  setLogs,
  setVersion,
  setUpdateStatus,
  setCheckingUpdate,
  setUpdating,
  setProgress,
  setCopied,
  setMainRunning,
  setLaunching,
  setLaunchPhase,
  resetProgress,
  addWebviewTab,
  removeWebviewTab,
} = launcherSlice.actions;

export default launcherSlice.reducer;
