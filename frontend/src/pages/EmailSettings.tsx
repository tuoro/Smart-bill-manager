import React, { useState, useEffect } from 'react';
import { 
  Card, Table, Button, Modal, Form, Input, InputNumber, Select, 
  message, Tag, Space, Popconfirm, Switch, Alert, Tooltip, Badge
} from 'antd';
import { 
  PlusOutlined, DeleteOutlined, PlayCircleOutlined, PauseCircleOutlined,
  ReloadOutlined, MailOutlined, CheckCircleOutlined, CloseCircleOutlined,
  QuestionCircleOutlined
} from '@ant-design/icons';
import { emailApi } from '../services/api';
import type { EmailConfig, EmailLog } from '../types';
import dayjs from 'dayjs';

const { Option } = Select;

// 常用邮箱IMAP配置
const EMAIL_PRESETS = [
  { name: 'QQ邮箱', host: 'imap.qq.com', port: 993 },
  { name: '163邮箱', host: 'imap.163.com', port: 993 },
  { name: '126邮箱', host: 'imap.126.com', port: 993 },
  { name: 'Gmail', host: 'imap.gmail.com', port: 993 },
  { name: 'Outlook', host: 'imap-mail.outlook.com', port: 993 },
  { name: '新浪邮箱', host: 'imap.sina.com', port: 993 },
];

const EmailSettings: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [configs, setConfigs] = useState<EmailConfig[]>([]);
  const [logs, setLogs] = useState<EmailLog[]>([]);
  const [monitorStatus, setMonitorStatus] = useState<Record<string, string>>({});
  const [modalVisible, setModalVisible] = useState(false);
  const [testLoading, setTestLoading] = useState(false);
  const [checkLoading, setCheckLoading] = useState<string | null>(null);
  const [form] = Form.useForm();

  useEffect(() => {
    loadConfigs();
    loadLogs();
    loadMonitorStatus();
  }, []);

  const loadConfigs = async () => {
    setLoading(true);
    try {
      const res = await emailApi.getConfigs();
      if (res.data.success && res.data.data) {
        setConfigs(res.data.data);
      }
    } catch (error) {
      message.error('加载邮箱配置失败');
    } finally {
      setLoading(false);
    }
  };

  const loadLogs = async () => {
    try {
      const res = await emailApi.getLogs(undefined, 50);
      if (res.data.success && res.data.data) {
        setLogs(res.data.data);
      }
    } catch (error) {
      console.error('Load logs failed:', error);
    }
  };

  const loadMonitorStatus = async () => {
    try {
      const res = await emailApi.getMonitoringStatus();
      if (res.data.success && res.data.data) {
        const statusMap: Record<string, string> = {};
        res.data.data.forEach(item => {
          statusMap[item.configId] = item.status;
        });
        setMonitorStatus(statusMap);
      }
    } catch (error) {
      console.error('Load monitor status failed:', error);
    }
  };

  const handleTest = async () => {
    try {
      const values = await form.validateFields();
      setTestLoading(true);
      const res = await emailApi.testConnection({
        email: values.email,
        imap_host: values.imap_host,
        imap_port: values.imap_port,
        password: values.password,
      });
      if (res.data.success) {
        message.success('连接测试成功！');
      } else {
        message.error(res.data.message || '连接测试失败');
      }
    } catch (error) {
      message.error('连接测试失败');
    } finally {
      setTestLoading(false);
    }
  };

  const handleSubmit = async (values: Omit<EmailConfig, 'id' | 'created_at' | 'last_check'>) => {
    try {
      await emailApi.createConfig(values);
      message.success('邮箱配置创建成功');
      setModalVisible(false);
      form.resetFields();
      loadConfigs();
      loadMonitorStatus();
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || '创建配置失败');
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await emailApi.deleteConfig(id);
      message.success('删除成功');
      loadConfigs();
      loadMonitorStatus();
    } catch (error) {
      message.error('删除失败');
    }
  };

  const handleStartMonitor = async (id: string) => {
    try {
      await emailApi.startMonitoring(id);
      message.success('监控已启动');
      loadMonitorStatus();
    } catch (error) {
      message.error('启动监控失败');
    }
  };

  const handleStopMonitor = async (id: string) => {
    try {
      await emailApi.stopMonitoring(id);
      message.success('监控已停止');
      loadMonitorStatus();
    } catch (error) {
      message.error('停止监控失败');
    }
  };

  const handleManualCheck = async (id: string) => {
    setCheckLoading(id);
    try {
      const res = await emailApi.manualCheck(id);
      if (res.data.success) {
        message.success(res.data.message || '检查完成');
        if (res.data.data && res.data.data.newEmails > 0) {
          loadLogs();
        }
      } else {
        message.error(res.data.message || '检查失败');
      }
    } catch (error) {
      message.error('检查邮件失败');
    } finally {
      setCheckLoading(null);
    }
  };

  const handlePresetSelect = (preset: string) => {
    const selected = EMAIL_PRESETS.find(p => p.name === preset);
    if (selected) {
      form.setFieldsValue({
        imap_host: selected.host,
        imap_port: selected.port,
      });
    }
  };

  const configColumns = [
    {
      title: '邮箱地址',
      dataIndex: 'email',
      key: 'email',
      render: (val: string) => (
        <Space>
          <MailOutlined style={{ color: '#1890ff' }} />
          {val}
        </Space>
      ),
    },
    {
      title: 'IMAP服务器',
      dataIndex: 'imap_host',
      key: 'imap_host',
    },
    {
      title: '端口',
      dataIndex: 'imap_port',
      key: 'imap_port',
    },
    {
      title: '状态',
      key: 'status',
      render: (_: unknown, record: EmailConfig) => {
        const status = monitorStatus[record.id];
        return status === 'running' ? (
          <Badge status="processing" text={<Tag color="success">监控中</Tag>} />
        ) : (
          <Tag color="default">已停止</Tag>
        );
      },
    },
    {
      title: '最后检查',
      dataIndex: 'last_check',
      key: 'last_check',
      render: (val: string) => val ? dayjs(val).format('MM-DD HH:mm') : '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 250,
      render: (_: unknown, record: EmailConfig) => {
        const isRunning = monitorStatus[record.id] === 'running';
        return (
          <Space>
            {isRunning ? (
              <Tooltip title="停止监控">
                <Button 
                  type="link" 
                  icon={<PauseCircleOutlined />}
                  onClick={() => handleStopMonitor(record.id)}
                  danger
                />
              </Tooltip>
            ) : (
              <Tooltip title="启动监控">
                <Button 
                  type="link" 
                  icon={<PlayCircleOutlined />}
                  onClick={() => handleStartMonitor(record.id)}
                  style={{ color: '#52c41a' }}
                />
              </Tooltip>
            )}
            <Tooltip title="手动检查">
              <Button 
                type="link" 
                icon={<ReloadOutlined />}
                loading={checkLoading === record.id}
                onClick={() => handleManualCheck(record.id)}
              />
            </Tooltip>
            <Popconfirm
              title="确定删除这个邮箱配置吗？"
              onConfirm={() => handleDelete(record.id)}
              okText="确定"
              cancelText="取消"
            >
              <Button type="link" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        );
      },
    },
  ];

  const logColumns = [
    {
      title: '主题',
      dataIndex: 'subject',
      key: 'subject',
      ellipsis: true,
    },
    {
      title: '发件人',
      dataIndex: 'from_address',
      key: 'from_address',
      ellipsis: true,
      width: 180,
    },
    {
      title: '附件',
      dataIndex: 'has_attachment',
      key: 'has_attachment',
      width: 100,
      render: (val: number, record: EmailLog) => 
        val ? (
          <Tag color="blue" icon={<CheckCircleOutlined />}>
            {record.attachment_count}个
          </Tag>
        ) : (
          <Tag icon={<CloseCircleOutlined />}>无</Tag>
        ),
    },
    {
      title: '接收时间',
      dataIndex: 'received_date',
      key: 'received_date',
      width: 150,
      render: (val: string) => val ? dayjs(val).format('MM-DD HH:mm') : '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (val: string) => (
        <Tag color={val === 'processed' ? 'success' : 'warning'}>
          {val === 'processed' ? '已处理' : val}
        </Tag>
      ),
    },
  ];

  return (
    <div>
      <Alert
        message="QQ邮箱配置说明"
        description={
          <div>
            <p>1. 登录QQ邮箱，进入「设置」→「账户」</p>
            <p>2. 找到「IMAP/SMTP服务」并开启</p>
            <p>3. 生成「授权码」（不是QQ密码）</p>
            <p>4. 在下方配置中使用邮箱地址和授权码</p>
          </div>
        }
        type="info"
        showIcon
        icon={<QuestionCircleOutlined />}
        style={{ marginBottom: 16 }}
      />

      <Card 
        title="邮箱配置"
        extra={
          <Button 
            type="primary" 
            icon={<PlusOutlined />}
            onClick={() => {
              form.resetFields();
              form.setFieldsValue({ imap_port: 993, is_active: 1 });
              setModalVisible(true);
            }}
          >
            添加邮箱
          </Button>
        }
        style={{ marginBottom: 16 }}
      >
        <Table
          dataSource={configs}
          columns={configColumns}
          rowKey="id"
          loading={loading}
          pagination={false}
          locale={{ emptyText: '暂无邮箱配置，请添加' }}
        />
      </Card>

      <Card 
        title="邮件处理日志"
        extra={
          <Button icon={<ReloadOutlined />} onClick={loadLogs}>
            刷新
          </Button>
        }
      >
        <Table
          dataSource={logs}
          columns={logColumns}
          rowKey="id"
          pagination={{
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条记录`,
          }}
          locale={{ emptyText: '暂无邮件处理记录' }}
        />
      </Card>

      <Modal
        title="添加邮箱配置"
        open={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          form.resetFields();
        }}
        footer={null}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          <Form.Item label="快速选择邮箱类型">
            <Select 
              placeholder="选择邮箱类型自动填充服务器配置"
              onChange={handlePresetSelect}
              allowClear
            >
              {EMAIL_PRESETS.map(p => (
                <Option key={p.name} value={p.name}>{p.name}</Option>
              ))}
            </Select>
          </Form.Item>

          <Form.Item
            name="email"
            label="邮箱地址"
            rules={[
              { required: true, message: '请输入邮箱地址' },
              { type: 'email', message: '请输入有效的邮箱地址' }
            ]}
          >
            <Input placeholder="example@qq.com" />
          </Form.Item>

          <Form.Item
            name="imap_host"
            label="IMAP服务器"
            rules={[{ required: true, message: '请输入IMAP服务器地址' }]}
          >
            <Input placeholder="imap.qq.com" />
          </Form.Item>

          <Form.Item
            name="imap_port"
            label="IMAP端口"
            rules={[{ required: true, message: '请输入端口号' }]}
          >
            <InputNumber style={{ width: '100%' }} placeholder="993" />
          </Form.Item>

          <Form.Item
            name="password"
            label="授权码/密码"
            rules={[{ required: true, message: '请输入授权码或密码' }]}
            extra="QQ邮箱请使用授权码，不是QQ密码"
          >
            <Input.Password placeholder="请输入授权码" />
          </Form.Item>

          <Form.Item
            name="is_active"
            label="启用状态"
            valuePropName="checked"
            initialValue={true}
          >
            <Switch checkedChildren="启用" unCheckedChildren="禁用" defaultChecked />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0, textAlign: 'right' }}>
            <Space>
              <Button onClick={handleTest} loading={testLoading}>
                测试连接
              </Button>
              <Button onClick={() => setModalVisible(false)}>取消</Button>
              <Button type="primary" htmlType="submit">
                保存配置
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default EmailSettings;
