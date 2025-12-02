import React, { useState, useEffect } from 'react';
import { Card, Row, Col, Statistic, Table, Tag, Progress, Empty, Spin } from 'antd';
import { 
  WalletOutlined, 
  FileTextOutlined, 
  MailOutlined, 
  RiseOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined
} from '@ant-design/icons';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts';
import { dashboardApi } from '../services/api';
import type { DashboardData, EmailLog } from '../types';
import dayjs from 'dayjs';

const COLORS = ['#1890ff', '#52c41a', '#faad14', '#f5222d', '#722ed1', '#13c2c2'];

const Dashboard: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<DashboardData | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    setLoading(true);
    try {
      const res = await dashboardApi.getSummary();
      if (res.data.success && res.data.data) {
        setData(res.data.data);
      }
    } catch (error) {
      console.error('Failed to load dashboard data:', error);
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%', padding: 100 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  if (!data) {
    return <Empty description="暂无数据" />;
  }

  // Prepare chart data
  const dailyData = Object.entries(data.payments.dailyStats || {}).map(([date, amount]) => ({
    date: dayjs(date).format('MM-DD'),
    amount
  })).sort((a, b) => a.date.localeCompare(b.date));

  const categoryData = Object.entries(data.payments.categoryStats || {}).map(([name, value]) => ({
    name,
    value
  }));

  const emailLogColumns = [
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
      width: 150,
    },
    {
      title: '附件',
      dataIndex: 'has_attachment',
      key: 'has_attachment',
      width: 80,
      render: (val: number, record: EmailLog) => 
        val ? <Tag color="blue">{record.attachment_count}个</Tag> : <Tag>无</Tag>
    },
    {
      title: '时间',
      dataIndex: 'received_date',
      key: 'received_date',
      width: 150,
      render: (val: string) => val ? dayjs(val).format('MM-DD HH:mm') : '-'
    },
  ];

  return (
    <div>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card 
            hoverable
            style={{ background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)', color: 'white' }}
          >
            <Statistic
              title={<span style={{ color: 'rgba(255,255,255,0.85)' }}>本月支出</span>}
              value={data.payments.totalThisMonth}
              precision={2}
              prefix={<WalletOutlined />}
              suffix="¥"
              valueStyle={{ color: 'white' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card 
            hoverable
            style={{ background: 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)', color: 'white' }}
          >
            <Statistic
              title={<span style={{ color: 'rgba(255,255,255,0.85)' }}>支付笔数</span>}
              value={data.payments.countThisMonth}
              prefix={<RiseOutlined />}
              suffix="笔"
              valueStyle={{ color: 'white' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card 
            hoverable
            style={{ background: 'linear-gradient(135deg, #4facfe 0%, #00f2fe 100%)', color: 'white' }}
          >
            <Statistic
              title={<span style={{ color: 'rgba(255,255,255,0.85)' }}>发票总数</span>}
              value={data.invoices.totalCount}
              prefix={<FileTextOutlined />}
              suffix="张"
              valueStyle={{ color: 'white' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card 
            hoverable
            style={{ background: 'linear-gradient(135deg, #43e97b 0%, #38f9d7 100%)', color: 'white' }}
          >
            <Statistic
              title={<span style={{ color: 'rgba(255,255,255,0.85)' }}>发票金额</span>}
              value={data.invoices.totalAmount}
              precision={2}
              prefix={<FileTextOutlined />}
              suffix="¥"
              valueStyle={{ color: 'white' }}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={16}>
          <Card title="每日支出趋势" extra={<a onClick={loadData}>刷新</a>}>
            {dailyData.length > 0 ? (
              <ResponsiveContainer width="100%" height={300}>
                <LineChart data={dailyData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="date" />
                  <YAxis />
                  <Tooltip formatter={(value: number) => `¥${value.toFixed(2)}`} />
                  <Line 
                    type="monotone" 
                    dataKey="amount" 
                    stroke="#1890ff" 
                    strokeWidth={2}
                    dot={{ fill: '#1890ff' }}
                    activeDot={{ r: 8 }}
                  />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" style={{ height: 300 }} />
            )}
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="支出分类" style={{ height: '100%' }}>
            {categoryData.length > 0 ? (
              <ResponsiveContainer width="100%" height={300}>
                <PieChart>
                  <Pie
                    data={categoryData}
                    cx="50%"
                    cy="50%"
                    labelLine={false}
                    label={({ name, percent }) => `${name} ${((percent || 0) * 100).toFixed(0)}%`}
                    outerRadius={80}
                    fill="#8884d8"
                    dataKey="value"
                  >
                    {categoryData.map((_entry, index) => (
                      <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip formatter={(value: number) => `¥${value.toFixed(2)}`} />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <Empty description="暂无数据" style={{ height: 300 }} />
            )}
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <Card 
            title={
              <span>
                <MailOutlined /> 邮箱监控状态
              </span>
            }
          >
            {data.email.monitoringStatus.length > 0 ? (
              data.email.monitoringStatus.map((item, index) => (
                <div key={index} style={{ marginBottom: 12, display: 'flex', alignItems: 'center' }}>
                  <span style={{ marginRight: 8 }}>邮箱 {index + 1}:</span>
                  {item.status === 'running' ? (
                    <Tag icon={<CheckCircleOutlined />} color="success">运行中</Tag>
                  ) : (
                    <Tag icon={<CloseCircleOutlined />} color="default">已停止</Tag>
                  )}
                  <Progress 
                    percent={item.status === 'running' ? 100 : 0} 
                    status={item.status === 'running' ? 'active' : 'exception'}
                    style={{ flex: 1, marginLeft: 16 }}
                    showInfo={false}
                  />
                </div>
              ))
            ) : (
              <Empty description="暂无配置邮箱" />
            )}
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="最近邮件">
            <Table 
              dataSource={data.email.recentLogs} 
              columns={emailLogColumns}
              pagination={false}
              size="small"
              rowKey="id"
              locale={{ emptyText: '暂无邮件记录' }}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col span={24}>
          <Card title="发票来源分布">
            <Row gutter={16}>
              {Object.entries(data.invoices.bySource || {}).map(([source, count], index) => (
                <Col key={source} xs={12} sm={8} md={6}>
                  <Card 
                    size="small" 
                    style={{ textAlign: 'center', background: `${COLORS[index % COLORS.length]}15` }}
                  >
                    <Statistic 
                      title={source === 'upload' ? '手动上传' : source === 'email' ? '邮件下载' : source}
                      value={count}
                      suffix="张"
                    />
                  </Card>
                </Col>
              ))}
              {Object.keys(data.invoices.bySource || {}).length === 0 && (
                <Col span={24}>
                  <Empty description="暂无发票" />
                </Col>
              )}
            </Row>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default Dashboard;
