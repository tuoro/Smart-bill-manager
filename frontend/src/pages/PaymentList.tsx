import React, { useState, useEffect } from 'react';
import { 
  Table, Button, Modal, Form, Input, InputNumber, Select, DatePicker, 
  Space, message, Popconfirm, Tag, Card, Row, Col, Statistic 
} from 'antd';
import { 
  PlusOutlined, EditOutlined, DeleteOutlined, 
  WalletOutlined, ShoppingOutlined 
} from '@ant-design/icons';
import { paymentApi } from '../services/api';
import type { Payment } from '../types';
import dayjs from 'dayjs';

const { Option } = Select;
const { RangePicker } = DatePicker;

const CATEGORIES = [
  '餐饮', '交通', '购物', '娱乐', '住房', '医疗', '教育', '通讯', '其他'
];

const PAYMENT_METHODS = [
  '微信支付', '支付宝', '银行卡', '现金', '信用卡', '其他'
];

const PaymentList: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [modalVisible, setModalVisible] = useState(false);
  const [editingPayment, setEditingPayment] = useState<Payment | null>(null);
  const [form] = Form.useForm();
  const [stats, setStats] = useState<{
    totalAmount: number;
    totalCount: number;
    categoryStats: Record<string, number>;
  } | null>(null);
  const [filters, setFilters] = useState<{
    dateRange?: [dayjs.Dayjs, dayjs.Dayjs];
    category?: string;
  }>({});

  useEffect(() => {
    loadPayments();
    loadStats();
  }, [filters]);

  const loadPayments = async () => {
    setLoading(true);
    try {
      const params: Record<string, string> = {};
      if (filters.dateRange) {
        params.startDate = filters.dateRange[0].startOf('day').toISOString();
        params.endDate = filters.dateRange[1].endOf('day').toISOString();
      }
      if (filters.category) {
        params.category = filters.category;
      }
      const res = await paymentApi.getAll(params);
      if (res.data.success && res.data.data) {
        setPayments(res.data.data);
      }
    } catch (error) {
      message.error('加载支付记录失败');
    } finally {
      setLoading(false);
    }
  };

  const loadStats = async () => {
    try {
      const startDate = filters.dateRange?.[0]?.startOf('day').toISOString();
      const endDate = filters.dateRange?.[1]?.endOf('day').toISOString();
      const res = await paymentApi.getStats(startDate, endDate);
      if (res.data.success && res.data.data) {
        setStats(res.data.data);
      }
    } catch (error) {
      console.error('Load stats failed:', error);
    }
  };

  const handleSubmit = async (values: Omit<Payment, 'id' | 'created_at'> & { transaction_time: dayjs.Dayjs }) => {
    try {
      const payload = {
        ...values,
        transaction_time: values.transaction_time.toISOString(),
      };

      if (editingPayment) {
        await paymentApi.update(editingPayment.id, payload);
        message.success('支付记录更新成功');
      } else {
        await paymentApi.create(payload);
        message.success('支付记录创建成功');
      }
      setModalVisible(false);
      form.resetFields();
      setEditingPayment(null);
      loadPayments();
      loadStats();
    } catch (error) {
      message.error('操作失败');
    }
  };

  const handleEdit = (payment: Payment) => {
    setEditingPayment(payment);
    form.setFieldsValue({
      ...payment,
      transaction_time: dayjs(payment.transaction_time),
    });
    setModalVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await paymentApi.delete(id);
      message.success('删除成功');
      loadPayments();
      loadStats();
    } catch (error) {
      message.error('删除失败');
    }
  };

  const columns = [
    {
      title: '金额',
      dataIndex: 'amount',
      key: 'amount',
      render: (val: number) => (
        <span style={{ color: '#f5222d', fontWeight: 'bold' }}>
          ¥{val.toFixed(2)}
        </span>
      ),
      sorter: (a: Payment, b: Payment) => a.amount - b.amount,
    },
    {
      title: '商家',
      dataIndex: 'merchant',
      key: 'merchant',
      ellipsis: true,
    },
    {
      title: '分类',
      dataIndex: 'category',
      key: 'category',
      render: (val: string) => val ? <Tag color="blue">{val}</Tag> : '-',
      filters: CATEGORIES.map(c => ({ text: c, value: c })),
      onFilter: (value: unknown, record: Payment) => record.category === value,
    },
    {
      title: '支付方式',
      dataIndex: 'payment_method',
      key: 'payment_method',
      render: (val: string) => val ? <Tag color="green">{val}</Tag> : '-',
    },
    {
      title: '备注',
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
    },
    {
      title: '交易时间',
      dataIndex: 'transaction_time',
      key: 'transaction_time',
      render: (val: string) => dayjs(val).format('YYYY-MM-DD HH:mm'),
      sorter: (a: Payment, b: Payment) => 
        new Date(a.transaction_time).getTime() - new Date(b.transaction_time).getTime(),
      defaultSortOrder: 'descend' as const,
    },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: (_: unknown, record: Payment) => (
        <Space>
          <Button 
            type="link" 
            icon={<EditOutlined />} 
            onClick={() => handleEdit(record)}
          />
          <Popconfirm
            title="确定删除这条记录吗？"
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
              title="总支出"
              value={stats?.totalAmount || 0}
              precision={2}
              prefix={<WalletOutlined />}
              suffix="¥"
              valueStyle={{ color: '#cf1322' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card>
            <Statistic
              title="交易笔数"
              value={stats?.totalCount || 0}
              prefix={<ShoppingOutlined />}
              suffix="笔"
              valueStyle={{ color: '#3f8600' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card>
            <Statistic
              title="平均每笔"
              value={stats?.totalCount ? (stats.totalAmount / stats.totalCount) : 0}
              precision={2}
              suffix="¥"
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
      </Row>

      <Card 
        title="支付记录"
        extra={
          <Space>
            <RangePicker 
              onChange={(dates) => setFilters(f => ({ 
                ...f, 
                dateRange: dates as [dayjs.Dayjs, dayjs.Dayjs] | undefined 
              }))}
              allowClear
            />
            <Select
              placeholder="选择分类"
              style={{ width: 120 }}
              allowClear
              onChange={(val) => setFilters(f => ({ ...f, category: val }))}
            >
              {CATEGORIES.map(c => (
                <Option key={c} value={c}>{c}</Option>
              ))}
            </Select>
            <Button 
              type="primary" 
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingPayment(null);
                form.resetFields();
                form.setFieldsValue({ transaction_time: dayjs() });
                setModalVisible(true);
              }}
            >
              添加记录
            </Button>
          </Space>
        }
      >
        <Table
          dataSource={payments}
          columns={columns}
          rowKey="id"
          loading={loading}
          pagination={{
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条记录`,
          }}
        />
      </Card>

      <Modal
        title={editingPayment ? '编辑支付记录' : '添加支付记录'}
        open={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          form.resetFields();
          setEditingPayment(null);
        }}
        footer={null}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          <Form.Item
            name="amount"
            label="金额"
            rules={[{ required: true, message: '请输入金额' }]}
          >
            <InputNumber
              prefix="¥"
              style={{ width: '100%' }}
              min={0}
              precision={2}
              placeholder="请输入金额"
            />
          </Form.Item>

          <Form.Item name="merchant" label="商家">
            <Input placeholder="请输入商家名称" />
          </Form.Item>

          <Form.Item name="category" label="分类">
            <Select placeholder="请选择分类" allowClear>
              {CATEGORIES.map(c => (
                <Option key={c} value={c}>{c}</Option>
              ))}
            </Select>
          </Form.Item>

          <Form.Item name="payment_method" label="支付方式">
            <Select placeholder="请选择支付方式" allowClear>
              {PAYMENT_METHODS.map(m => (
                <Option key={m} value={m}>{m}</Option>
              ))}
            </Select>
          </Form.Item>

          <Form.Item name="description" label="备注">
            <Input.TextArea rows={2} placeholder="请输入备注" />
          </Form.Item>

          <Form.Item
            name="transaction_time"
            label="交易时间"
            rules={[{ required: true, message: '请选择交易时间' }]}
          >
            <DatePicker 
              showTime 
              style={{ width: '100%' }} 
              placeholder="请选择交易时间"
            />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0, textAlign: 'right' }}>
            <Space>
              <Button onClick={() => setModalVisible(false)}>取消</Button>
              <Button type="primary" htmlType="submit">
                {editingPayment ? '更新' : '添加'}
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default PaymentList;
