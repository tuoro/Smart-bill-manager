import React, { useState, useEffect } from 'react';
import { ConfigProvider, Layout, Menu, theme, Dropdown, Avatar, message, Spin } from 'antd';
import {
  DashboardOutlined,
  WalletOutlined,
  FileTextOutlined,
  MailOutlined,
  RobotOutlined,
  UserOutlined,
  LogoutOutlined,
  KeyOutlined,
} from '@ant-design/icons';
import zhCN from 'antd/locale/zh_CN';

import Dashboard from './pages/Dashboard';
import PaymentList from './pages/PaymentList';
import InvoiceList from './pages/InvoiceList';
import EmailSettings from './pages/EmailSettings';
import DingtalkSettings from './pages/DingtalkSettings';
import Login from './pages/Login';

import { getStoredUser, clearAuth, authApi } from './services/api';
import type { User } from './types';

import './App.css';

const { Header, Sider, Content } = Layout;

const menuItems = [
  {
    key: 'dashboard',
    icon: <DashboardOutlined />,
    label: '仪表盘',
  },
  {
    key: 'payments',
    icon: <WalletOutlined />,
    label: '支付记录',
  },
  {
    key: 'invoices',
    icon: <FileTextOutlined />,
    label: '发票管理',
  },
  {
    key: 'email',
    icon: <MailOutlined />,
    label: '邮箱监控',
  },
  {
    key: 'dingtalk',
    icon: <RobotOutlined />,
    label: '钉钉机器人',
  },
];

const App: React.FC = () => {
  const [currentPage, setCurrentPage] = useState('dashboard');
  const [collapsed, setCollapsed] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    checkAuth();
  }, []);

  const checkAuth = async () => {
    const storedUser = getStoredUser();
    if (storedUser) {
      try {
        // Verify token is still valid
        await authApi.verify();
        setUser(storedUser);
      } catch {
        // Token invalid, clear auth
        clearAuth();
        setUser(null);
      }
    }
    setLoading(false);
  };

  const handleLoginSuccess = (loggedInUser: User) => {
    setUser(loggedInUser);
  };

  const handleLogout = () => {
    clearAuth();
    setUser(null);
    message.success('已退出登录');
  };

  const userMenuItems = [
    {
      key: 'profile',
      icon: <UserOutlined />,
      label: user?.username || '用户',
      disabled: true,
    },
    {
      key: 'change-password',
      icon: <KeyOutlined />,
      label: '修改密码',
    },
    {
      type: 'divider' as const,
    },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      danger: true,
    },
  ];

  const handleUserMenuClick = ({ key }: { key: string }) => {
    if (key === 'logout') {
      handleLogout();
    } else if (key === 'change-password') {
      message.info('修改密码功能开发中...');
    }
  };

  const renderContent = () => {
    switch (currentPage) {
      case 'dashboard':
        return <Dashboard />;
      case 'payments':
        return <PaymentList />;
      case 'invoices':
        return <InvoiceList />;
      case 'email':
        return <EmailSettings />;
      case 'dingtalk':
        return <DingtalkSettings />;
      default:
        return <Dashboard />;
    }
  };

  if (loading) {
    return (
      <div style={{
        minHeight: '100vh',
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        background: '#f0f2f5',
      }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  if (!user) {
    return (
      <ConfigProvider
        locale={zhCN}
        theme={{
          algorithm: theme.defaultAlgorithm,
          token: {
            colorPrimary: '#1890ff',
            borderRadius: 6,
          },
        }}
      >
        <Login onLoginSuccess={handleLoginSuccess} />
      </ConfigProvider>
    );
  }

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          colorPrimary: '#1890ff',
          borderRadius: 6,
        },
      }}
    >
      <Layout style={{ minHeight: '100vh' }}>
        <Sider 
          collapsible 
          collapsed={collapsed} 
          onCollapse={setCollapsed}
          style={{
            background: 'linear-gradient(180deg, #001529 0%, #003a70 100%)',
          }}
        >
          <div style={{ 
            height: 64, 
            display: 'flex', 
            alignItems: 'center', 
            justifyContent: 'center',
            color: 'white',
            fontSize: collapsed ? 16 : 18,
            fontWeight: 'bold',
            borderBottom: '1px solid rgba(255,255,255,0.1)',
          }}>
            {collapsed ? '💰' : '💰 智能账单管理'}
          </div>
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[currentPage]}
            items={menuItems}
            onClick={({ key }) => setCurrentPage(key)}
            style={{ background: 'transparent' }}
          />
        </Sider>
        <Layout>
          <Header style={{ 
            padding: '0 24px', 
            background: '#fff', 
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            boxShadow: '0 1px 4px rgba(0,21,41,.08)',
          }}>
            <h2 style={{ margin: 0, color: '#1890ff' }}>
              {menuItems.find(m => m.key === currentPage)?.label || '仪表盘'}
            </h2>
            <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
              <Dropdown 
                menu={{ items: userMenuItems, onClick: handleUserMenuClick }}
                placement="bottomRight"
              >
                <div style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Avatar 
                    style={{ backgroundColor: '#1890ff' }} 
                    icon={<UserOutlined />}
                  />
                  <span>{user.username}</span>
                </div>
              </Dropdown>
            </div>
          </Header>
          <Content style={{ 
            margin: 24, 
            padding: 24, 
            background: '#f0f2f5',
            minHeight: 280,
            borderRadius: 8,
          }}>
            {renderContent()}
          </Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
};

export default App;
