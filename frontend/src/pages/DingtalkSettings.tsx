import React, { useState, useEffect } from 'react';
import { 
  Card, Table, Button, Modal, Form, Input, 
  message, Tag, Space, Popconfirm, Switch, Alert, Typography, Tooltip
} from 'antd';
import { 
  PlusOutlined, DeleteOutlined, CopyOutlined,
  RobotOutlined, CheckCircleOutlined, CloseCircleOutlined
} from '@ant-design/icons';
import { dingtalkApi } from '../services/api';
import type { DingtalkConfig, DingtalkLog } from '../types';
import dayjs from 'dayjs';

const { Text, Paragraph } = Typography;

const DingtalkSettings: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [configs, setConfigs] = useState<DingtalkConfig[]>([]);
  const [logs, setLogs] = useState<DingtalkLog[]>([]);
  const [modalVisible, setModalVisible] = useState(false);
  const [form] = Form.useForm();

  useEffect(() => {
    loadConfigs();
    loadLogs();
  }, []);

  const loadConfigs = async () => {
    setLoading(true);
    try {
      const res = await dingtalkApi.getConfigs();
      if (res.data.success && res.data.data) {
        setConfigs(res.data.data);
      }
    } catch {
      message.error('加载钉钉配置失败');
    } finally {
      setLoading(false);
    }
  };

  const loadLogs = async () => {
    try {
      const res = await dingtalkApi.getLogs(undefined, 50);
      if (res.data.success && res.data.data) {
        setLogs(res.data.data);
      }
    } catch (error) {
      console.error('Load logs failed:', error);
    }
  };

  const handleSubmit = async (values: Omit<DingtalkConfig, 'id' | 'created_at'>) => {
    try {
      await dingtalkApi.createConfig(values);
      message.success('钉钉机器人配置创建成功');
      setModalVisible(false);
      form.resetFields();
      loadConfigs();
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || '创建配置失败');
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await dingtalkApi.deleteConfig(id);
      message.success('删除成功');
      loadConfigs();
    } catch {
      message.error('删除失败');
    }
  };

  const copyWebhookUrl = (id: string) => {
    const baseUrl = window.location.origin.replace(/:\d+$/, ':3001');
    const webhookUrl = `${baseUrl}/api/dingtalk/webhook/${id}`;
    navigator.clipboard.writeText(webhookUrl).then(() => {
      message.success('Webhook URL已复制到剪贴板');
    }).catch(() => {
      message.info(`Webhook URL: ${webhookUrl}`);
    });
  };

  const configColumns = [
    {
      title: '配置名称',
      dataIndex: 'name',
      key: 'name',
      render: (val: string) => (
        <Space>
          <RobotOutlined style={{ color: '#1890ff' }} />
          {val}
        </Space>
      ),
    },
    {
      title: 'App Key',
      dataIndex: 'app_key',
      key: 'app_key',
      render: (val: string) => val ? <Text code>{val.substring(0, 8)}...</Text> : '-',
    },
    {
      title: '状态',
      dataIndex: 'is_active',
      key: 'is_active',
      render: (val: number) => (
        val ? <Tag color="success">启用</Tag> : <Tag color="default">禁用</Tag>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (val: string) => val ? dayjs(val).format('YYYY-MM-DD HH:mm') : '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 200,
      render: (_: unknown, record: DingtalkConfig) => (
        <Space>
          <Tooltip title="复制Webhook URL">
            <Button 
              type="link" 
              icon={<CopyOutlined />}
              onClick={() => copyWebhookUrl(record.id)}
            />
          </Tooltip>
          <Popconfirm
            title="确定删除这个钉钉配置吗？"
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
          >
            <Button type="link" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const logColumns = [
    {
      title: '消息类型',
      dataIndex: 'message_type',
      key: 'message_type',
      width: 100,
      render: (val: string) => <Tag>{val || 'unknown'}</Tag>,
    },
    {
      title: '发送者',
      dataIndex: 'sender_nick',
      key: 'sender_nick',
      width: 120,
    },
    {
      title: '内容',
      dataIndex: 'content',
      key: 'content',
      ellipsis: true,
    },
    {
      title: '附件',
      dataIndex: 'has_attachment',
      key: 'has_attachment',
      width: 80,
      render: (val: number, record: DingtalkLog) => 
        val ? (
          <Tag color="blue" icon={<CheckCircleOutlined />}>
            {record.attachment_count}个
          </Tag>
        ) : (
          <Tag icon={<CloseCircleOutlined />}>无</Tag>
        ),
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 150,
      render: (val: string) => val ? dayjs(val).format('MM-DD HH:mm') : '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
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
        message="钉钉机器人配置说明"
        description={
          <div>
            <Paragraph>
              <Text strong>1. 创建钉钉机器人：</Text>
              <br />
              在钉钉群设置中添加「自定义机器人」，获取Webhook地址和安全设置
            </Paragraph>
            <Paragraph>
              <Text strong>2. 配置机器人：</Text>
              <br />
              - 如需下载文件功能，请在钉钉开放平台创建企业内部应用获取App Key和App Secret
              <br />
              - 如需签名验证，请配置Webhook Token（机器人安全设置中的加签密钥）
            </Paragraph>
            <Paragraph>
              <Text strong>3. 设置回调地址：</Text>
              <br />
              创建配置后，复制Webhook URL设置到钉钉机器人的消息接收地址
            </Paragraph>
            <Paragraph>
              <Text strong>4. 发送发票：</Text>
              <br />
              在钉钉群中@机器人并发送PDF发票文件，系统将自动解析并保存
            </Paragraph>
          </div>
        }
        type="info"
        showIcon
        icon={<RobotOutlined />}
        style={{ marginBottom: 16 }}
      />

      <Card 
        title="钉钉机器人配置"
        extra={
          <Button 
            type="primary" 
            icon={<PlusOutlined />}
            onClick={() => {
              form.resetFields();
              form.setFieldsValue({ is_active: 1 });
              setModalVisible(true);
            }}
          >
            添加机器人
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
          locale={{ emptyText: '暂无钉钉机器人配置，请添加' }}
        />
      </Card>

      <Card 
        title="消息处理日志"
        extra={
          <Button icon={<CopyOutlined />} onClick={loadLogs}>
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
          locale={{ emptyText: '暂无消息处理记录' }}
        />
      </Card>

      <Modal
        title="添加钉钉机器人配置"
        open={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          form.resetFields();
        }}
        footer={null}
        destroyOnClose
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          <Form.Item
            name="name"
            label="配置名称"
            rules={[{ required: true, message: '请输入配置名称' }]}
          >
            <Input placeholder="例如：发票收集机器人" />
          </Form.Item>

          <Form.Item
            name="app_key"
            label="App Key"
            extra="可选。如需下载文件功能，请在钉钉开放平台创建应用获取"
          >
            <Input placeholder="钉钉应用App Key（可选）" />
          </Form.Item>

          <Form.Item
            name="app_secret"
            label="App Secret"
            extra="可选。与App Key配合使用，用于获取访问令牌"
          >
            <Input.Password placeholder="钉钉应用App Secret（可选）" />
          </Form.Item>

          <Form.Item
            name="webhook_token"
            label="Webhook Token (加签密钥)"
            extra="可选。如果机器人启用了加签验证，请填写加签密钥"
          >
            <Input.Password placeholder="机器人加签密钥（可选）" />
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

export default DingtalkSettings;
