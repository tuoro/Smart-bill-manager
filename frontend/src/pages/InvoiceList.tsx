import React, { useState, useEffect } from 'react';
import { 
  Table, Button, Upload, message, Card, Space, Tag, Popconfirm, 
  Modal, Descriptions, Row, Col, Statistic, Empty
} from 'antd';
import { 
  UploadOutlined, DeleteOutlined, EyeOutlined, FileTextOutlined,
  CloudDownloadOutlined, InboxOutlined
} from '@ant-design/icons';
import type { UploadProps, UploadFile } from 'antd';
import { invoiceApi } from '../services/api';
import type { Invoice } from '../types';
import dayjs from 'dayjs';

const { Dragger } = Upload;

const InvoiceList: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [previewVisible, setPreviewVisible] = useState(false);
  const [previewInvoice, setPreviewInvoice] = useState<Invoice | null>(null);
  const [stats, setStats] = useState<{
    totalCount: number;
    totalAmount: number;
    bySource: Record<string, number>;
  } | null>(null);
  const [uploadModalVisible, setUploadModalVisible] = useState(false);
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const [uploading, setUploading] = useState(false);

  useEffect(() => {
    loadInvoices();
    loadStats();
  }, []);

  const loadInvoices = async () => {
    setLoading(true);
    try {
      const res = await invoiceApi.getAll();
      if (res.data.success && res.data.data) {
        setInvoices(res.data.data);
      }
    } catch (error) {
      message.error('加载发票列表失败');
    } finally {
      setLoading(false);
    }
  };

  const loadStats = async () => {
    try {
      const res = await invoiceApi.getStats();
      if (res.data.success && res.data.data) {
        setStats(res.data.data);
      }
    } catch (error) {
      console.error('Load stats failed:', error);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await invoiceApi.delete(id);
      message.success('删除成功');
      loadInvoices();
      loadStats();
    } catch (error) {
      message.error('删除失败');
    }
  };

  const handleUpload = async () => {
    if (fileList.length === 0) {
      message.warning('请选择文件');
      return;
    }

    setUploading(true);
    try {
      const files = fileList.map(f => f.originFileObj as File);
      if (files.length === 1) {
        await invoiceApi.upload(files[0]);
      } else {
        await invoiceApi.uploadMultiple(files);
      }
      message.success('上传成功');
      setFileList([]);
      setUploadModalVisible(false);
      loadInvoices();
      loadStats();
    } catch (error) {
      message.error('上传失败');
    } finally {
      setUploading(false);
    }
  };

  const uploadProps: UploadProps = {
    multiple: true,
    accept: '.pdf',
    fileList,
    beforeUpload: (file) => {
      if (file.type !== 'application/pdf') {
        message.error('只支持PDF文件');
        return Upload.LIST_IGNORE;
      }
      return false;
    },
    onChange: ({ fileList }) => {
      setFileList(fileList);
    },
    onRemove: (file) => {
      setFileList(prev => prev.filter(f => f.uid !== file.uid));
    },
  };

  const columns = [
    {
      title: '文件名',
      dataIndex: 'original_name',
      key: 'original_name',
      ellipsis: true,
      render: (val: string) => (
        <Space>
          <FileTextOutlined style={{ color: '#1890ff' }} />
          {val}
        </Space>
      ),
    },
    {
      title: '发票号码',
      dataIndex: 'invoice_number',
      key: 'invoice_number',
      render: (val: string) => val || '-',
    },
    {
      title: '金额',
      dataIndex: 'amount',
      key: 'amount',
      render: (val: number) => val ? (
        <span style={{ color: '#f5222d', fontWeight: 'bold' }}>
          ¥{val.toFixed(2)}
        </span>
      ) : '-',
    },
    {
      title: '销售方',
      dataIndex: 'seller_name',
      key: 'seller_name',
      ellipsis: true,
      render: (val: string) => val || '-',
    },
    {
      title: '来源',
      dataIndex: 'source',
      key: 'source',
      render: (val: string) => (
        <Tag color={val === 'email' ? 'blue' : 'green'}>
          {val === 'email' ? '邮件下载' : '手动上传'}
        </Tag>
      ),
    },
    {
      title: '上传时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (val: string) => dayjs(val).format('YYYY-MM-DD HH:mm'),
      sorter: (a: Invoice, b: Invoice) => 
        new Date(a.created_at || '').getTime() - new Date(b.created_at || '').getTime(),
      defaultSortOrder: 'descend' as const,
    },
    {
      title: '操作',
      key: 'action',
      width: 150,
      render: (_: unknown, record: Invoice) => (
        <Space>
          <Button 
            type="link" 
            icon={<EyeOutlined />}
            onClick={() => {
              setPreviewInvoice(record);
              setPreviewVisible(true);
            }}
          />
          <Button
            type="link"
            icon={<CloudDownloadOutlined />}
            onClick={() => {
              window.open(`http://localhost:3001/${record.file_path}`, '_blank');
            }}
          />
          <Popconfirm
            title="确定删除这张发票吗？"
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

  return (
    <div>
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} sm={8}>
          <Card>
            <Statistic
              title="发票总数"
              value={stats?.totalCount || 0}
              prefix={<FileTextOutlined />}
              suffix="张"
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card>
            <Statistic
              title="发票总金额"
              value={stats?.totalAmount || 0}
              precision={2}
              suffix="¥"
              valueStyle={{ color: '#cf1322' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card>
            <div style={{ display: 'flex', justifyContent: 'space-around' }}>
              <Statistic
                title="手动上传"
                value={stats?.bySource?.upload || 0}
                suffix="张"
                valueStyle={{ fontSize: 20 }}
              />
              <Statistic
                title="邮件下载"
                value={stats?.bySource?.email || 0}
                suffix="张"
                valueStyle={{ fontSize: 20 }}
              />
            </div>
          </Card>
        </Col>
      </Row>

      <Card 
        title="发票列表"
        extra={
          <Button 
            type="primary" 
            icon={<UploadOutlined />}
            onClick={() => setUploadModalVisible(true)}
          >
            上传发票
          </Button>
        }
      >
        <Table
          dataSource={invoices}
          columns={columns}
          rowKey="id"
          loading={loading}
          pagination={{
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 张发票`,
          }}
          locale={{ emptyText: <Empty description="暂无发票" /> }}
        />
      </Card>

      <Modal
        title="上传发票"
        open={uploadModalVisible}
        onCancel={() => {
          setUploadModalVisible(false);
          setFileList([]);
        }}
        footer={[
          <Button key="cancel" onClick={() => {
            setUploadModalVisible(false);
            setFileList([]);
          }}>
            取消
          </Button>,
          <Button 
            key="upload" 
            type="primary" 
            loading={uploading}
            onClick={handleUpload}
            disabled={fileList.length === 0}
          >
            上传
          </Button>,
        ]}
      >
        <Dragger {...uploadProps}>
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">点击或拖拽文件到此区域上传</p>
          <p className="ant-upload-hint">
            支持单个或批量上传PDF发票文件，系统将自动解析发票信息
          </p>
        </Dragger>
      </Modal>

      <Modal
        title="发票详情"
        open={previewVisible}
        onCancel={() => {
          setPreviewVisible(false);
          setPreviewInvoice(null);
        }}
        footer={null}
        width={700}
      >
        {previewInvoice && (
          <Descriptions bordered column={2}>
            <Descriptions.Item label="文件名" span={2}>
              {previewInvoice.original_name}
            </Descriptions.Item>
            <Descriptions.Item label="发票号码">
              {previewInvoice.invoice_number || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="开票日期">
              {previewInvoice.invoice_date || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="金额">
              {previewInvoice.amount ? `¥${previewInvoice.amount.toFixed(2)}` : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="文件大小">
              {previewInvoice.file_size ? `${(previewInvoice.file_size / 1024).toFixed(2)} KB` : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="销售方" span={2}>
              {previewInvoice.seller_name || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="购买方" span={2}>
              {previewInvoice.buyer_name || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="来源">
              <Tag color={previewInvoice.source === 'email' ? 'blue' : 'green'}>
                {previewInvoice.source === 'email' ? '邮件下载' : '手动上传'}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="上传时间">
              {dayjs(previewInvoice.created_at).format('YYYY-MM-DD HH:mm:ss')}
            </Descriptions.Item>
            <Descriptions.Item label="预览" span={2}>
              <Button 
                type="primary" 
                onClick={() => window.open(`http://localhost:3001/${previewInvoice.file_path}`, '_blank')}
              >
                查看PDF文件
              </Button>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default InvoiceList;
