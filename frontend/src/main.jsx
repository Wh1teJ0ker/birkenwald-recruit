import React from 'react';
import { createRoot } from 'react-dom/client';
import { App as AntdApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import App from './App.jsx';
import './styles.css';

const theme = {
  token: {
    colorPrimary: '#5f8378',
    colorInfo: '#6b7c93',
    colorSuccess: '#5f8378',
    colorWarning: '#8a6b3f',
    colorError: '#8f5555',
    colorLink: '#2e3540',
    colorText: '#2e3540',
    colorTextHeading: '#2e3540',
    colorTextSecondary: '#5b6573',
    colorTextTertiary: '#7c8797',
    colorTextQuaternary: '#a3adb9',
    colorBorder: '#d5dae1',
    colorBorderSecondary: 'rgba(46, 53, 64, 0.08)',
    colorBgLayout: '#e9eaee',
    colorBgContainer: '#eef0f4',
    colorBgElevated: '#f4f5f8',
    colorFillAlter: '#e4e6eb',
    colorFillSecondary: 'rgba(46, 53, 64, 0.05)',
    borderRadius: 12,
    borderRadiusLG: 24,
    controlHeight: 44,
    controlHeightLG: 48,
    controlHeightSM: 36,
    fontFamily:
      '"IBM Plex Sans", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans SC Variable", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
    fontSize: 14,
    boxShadow: '-6px -6px 16px rgba(255, 255, 255, 0.70), 8px 10px 20px rgba(182, 189, 201, 0.44), 2px 3px 5px rgba(182, 189, 201, 0.20)',
    boxShadowSecondary: '-8px -8px 22px rgba(255, 255, 255, 0.60), 12px 16px 30px rgba(168, 177, 193, 0.40), 3px 4px 9px rgba(168, 177, 193, 0.20)',
    wireframe: false,
  },
  components: {
    Button: {
      fontWeight: 500,
      borderRadius: 999,
      borderRadiusLG: 999,
      borderRadiusSM: 999,
      paddingInline: 22,
      paddingInlineLG: 26,
      primaryShadow: 'none',
      defaultShadow: 'none',
      dangerShadow: 'none',
    },
    Input: {
      paddingBlock: 10,
      paddingInline: 14,
      activeShadow: 'none',
      hoverBorderColor: 'transparent',
      activeBorderColor: 'transparent',
    },
    InputNumber: {
      paddingBlock: 10,
      paddingInline: 14,
      activeShadow: 'none',
      hoverBorderColor: 'transparent',
      activeBorderColor: 'transparent',
    },
    Select: {
      optionSelectedBg: '#eef0f4',
      optionSelectedFontWeight: 500,
      optionHeight: 38,
      hoverBorderColor: 'transparent',
      activeBorderColor: 'transparent',
    },
    DatePicker: {
      activeShadow: 'none',
      hoverBorderColor: 'transparent',
      activeBorderColor: 'transparent',
    },
    Switch: { colorPrimary: '#5f8378', trackHeight: 26, trackMinWidth: 48 },
    Table: {
      headerBg: '#e4e6eb',
      headerColor: '#5b6573',
      headerSplitColor: 'transparent',
      borderColor: 'rgba(46, 53, 64, 0.07)',
      rowHoverBg: 'rgba(255, 255, 255, 0.34)',
      cellPaddingBlock: 14,
      cellPaddingInline: 18,
    },
    Form: { labelColor: '#5b6573', labelFontSize: 12 },
    Spin: { colorPrimary: '#5f8378' },
    Message: { contentBg: '#f4f5f8', colorText: '#2e3540' },
    Pagination: { itemActiveBg: '#eef0f4' },
  },
};

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={theme}>
      <AntdApp><App /></AntdApp>
    </ConfigProvider>
  </React.StrictMode>,
);
