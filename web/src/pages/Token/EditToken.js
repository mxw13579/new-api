import React, { useEffect, useState, useContext, useRef } from 'react';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
  renderGroupOption,
  renderQuotaWithPrompt,
  getModelCategories,
} from '../../helpers';
import { useIsMobile } from '../../hooks/useIsMobile.js';
import {
  Button,
  SideSheet,
  Space,
  Spin,
  Typography,
  Card,
  Tag,
  Avatar,
  Form,
  Col,
  Row,
} from '@douyinfe/semi-ui';
import {
  IconCreditCard,
  IconLink,
  IconSave,
  IconClose,
  IconKey,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { StatusContext } from '../../context/Status';

const { Text, Title } = Typography;
import { Modal, Button as AntdButton, Input as AntdInput, message } from 'antd';

const EditToken = (props) => {
  const { t } = useTranslation();
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [loading, setLoading] = useState(false);
  const isMobile = useIsMobile();
  const formApiRef = useRef(null);
  const [models, setModels] = useState([]);
  const [groups, setGroups] = useState([]);
  const isEdit = props.editingToken.id !== undefined;

  const getInitValues = () => ({
    name: '',
    remain_quota: 500000,
    expired_time: -1,
    unlimited_quota: false,
    model_limits_enabled: false,
    model_limits: [],
    allow_ips: '',
    group: '',
    interval_quota: isEdit ? 0 : 500000,
    interval_time: 0,
    trigger_last_time: 0,
    interval_unit: 0,
    tokenCount: 1,
  });
  const [inputs, setInputs] = useState(originInputs);
  const {
    name,
    remain_quota,
    expired_time,
    unlimited_quota,
    model_limits_enabled,
    model_limits,
    allow_ips,
    group,
    interval_quota,
    interval_time,
    trigger_last_time,
    interval_unit
  } = inputs;
  // const [visible, setVisible] = useState(false);
  const [models, setModels] = useState([]);
  const [groups, setGroups] = useState([]);
  const navigate = useNavigate();
  const { t } = useTranslation();
  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };
  const handleCancel = () => {
    props.handleClose();
  };

  const setExpiredTime = (month, day, hour, minute) => {
    let now = new Date();
    let timestamp = now.getTime() / 1000;
    let seconds = month * 30 * 24 * 60 * 60;
    seconds += day * 24 * 60 * 60;
    seconds += hour * 60 * 60;
    seconds += minute * 60;
    if (!formApiRef.current) return;
    if (seconds !== 0) {
      timestamp += seconds;
      formApiRef.current.setValue('expired_time', timestamp2string(timestamp));
    } else {
      formApiRef.current.setValue('expired_time', -1);
    }
  };


  const setIntervalUnit = (month, day, hour, minute) => {
    let now = new Date();
    let timestamp = now.getTime() / 1000;
    let seconds = month * 30 * 24 * 60 * 60;
    seconds += day * 24 * 60 * 60;
    seconds += hour * 60 * 60;
    seconds += minute * 60;
    if (seconds !== 0) {
      timestamp += seconds;
      setInputs({ ...inputs, expired_time: timestamp2string(timestamp) });
    } else {
      setInputs({ ...inputs, expired_time: -1 });
    }
  };

  const setUnlimitedQuota = () => {
    setInputs({ ...inputs, unlimited_quota: !unlimited_quota });
  };

  const loadModels = async () => {
    let res = await API.get(`/api/user/models`);
    const { success, message, data } = res.data;
    if (success) {
      const categories = getModelCategories(t);
      let localModelOptions = data.map((model) => {
        let icon = null;
        for (const [key, category] of Object.entries(categories)) {
          if (key !== 'all' && category.filter({ model_name: model })) {
            icon = category.icon;
            break;
          }
        }
        return {
          label: (
            <span className="flex items-center gap-1">
              {icon}
              {model}
            </span>
          ),
          value: model,
        };
      });
      setModels(localModelOptions);
    } else {
      showError(t(message));
    }
  };

  const loadGroups = async () => {
    let res = await API.get(`/api/user/self/groups`);
    const { success, message, data } = res.data;
    if (success) {
      let localGroupOptions = Object.entries(data).map(([group, info]) => ({
        label: info.desc,
        value: group,
        ratio: info.ratio,
      }));
      if (statusState?.status?.default_use_auto_group) {
        if (localGroupOptions.some((group) => group.value === 'auto')) {
          localGroupOptions.sort((a, b) => (a.value === 'auto' ? -1 : 1));
        } else {
          localGroupOptions.unshift({ label: t('自动选择'), value: 'auto' });
        }
      }
      setGroups(localGroupOptions);
      if (statusState?.status?.default_use_auto_group && formApiRef.current) {
        formApiRef.current.setValue('group', 'auto');
      }
    } else {
      showError(t(message));
    }
  };

  const loadToken = async () => {
    setLoading(true);
    let res = await API.get(`/api/token/${props.editingToken.id}`);
    const { success, message, data } = res.data;
    if (success) {
      if (data.expired_time !== -1) {
        data.expired_time = timestamp2string(data.expired_time);
      }
      if (data.model_limits !== '') {
        data.model_limits = data.model_limits.split(',');
      } else {
        data.model_limits = [];
      }
      if (formApiRef.current) {
        formApiRef.current.setValues({ ...getInitValues(), ...data });
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  useEffect(() => {
    if (formApiRef.current) {
      if (!isEdit) {
        formApiRef.current.setValues(getInitValues());
      }
    }
    loadModels();
    loadGroups();
  }, [props.editingToken.id]);

  useEffect(() => {
    if (props.visiable) {
      if (isEdit) {
        loadToken();
      } else {
        formApiRef.current?.setValues(getInitValues());
      }
    } else {
      formApiRef.current?.reset();
    }
  }, [props.visiable, props.editingToken.id]);

  const generateRandomSuffix = () => {
    const characters =
      'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    let result = '';
    for (let i = 0; i < 6; i++) {
      result += characters.charAt(
        Math.floor(Math.random() * characters.length),
      );
    }
    return result;
  };


  const showKeysDialog = (keys) => {
    const keysText = keys.join('\n');
    Modal.info({
      title: '令牌批量创建成功',
      width: 600,
      content: (
        <div>
          <AntdInput.TextArea
            value={keysText}
            readOnly
            autoSize={{ minRows: 6, maxRows: 16 }}
            style={{ marginBottom: 8, fontFamily: 'monospace' }}
          />
          <AntdButton
            type="primary"
            style={{ marginTop: 8 }}
            onClick={() => {
              navigator.clipboard.writeText(keysText);
              message.success('已复制到剪贴板！');
            }}
            block
          >
            复制所有Key
          </AntdButton>
        </div>
      ),
      okText: "知道了",
    });
  };

  const submit = async () => {
    setLoading(true);

    if (isEdit) {
      // 编辑令牌的逻辑保持不变
      let localInputs = { ...inputs };
      localInputs.remain_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_time = parseInt(localInputs.interval_time || -1);
      localInputs.interval_unit = parseInt(localInputs.interval_unit || 3);

      if (localInputs.expired_time !== -1) {
        let time = Date.parse(localInputs.expired_time);
        if (isNaN(time)) {
          showError(t('过期时间格式错误！'));
          setLoading(false);
          return;
        }
        localInputs.expired_time = Math.ceil(time / 1000);
      }
      localInputs.model_limits = localInputs.model_limits.join(',');
      localInputs.model_limits_enabled = localInputs.model_limits.length > 0;
      let res = await API.put(`/api/token/`, {
        ...localInputs,
        id: parseInt(props.editingToken.id),
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('令牌更新成功！'));
        props.refresh();
        props.handleClose();
      } else {
        showError(t(message));
      }

    } else if (tokenCount === 1) {
      // 单一令牌添加
      let localInputs = { ...inputs };
      localInputs.remain_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_time = parseInt(localInputs.interval_time || -1);
      localInputs.interval_unit = parseInt(localInputs.interval_unit || 3);
      localInputs.expired_time = -1;
      localInputs.model_limits = localInputs.model_limits.join(',');

      let res = await API.post(`/api/token/`, localInputs);
      const { success, message, keys } = res.data;
      if (success) {
        // keys 可能有可能没有，兼容一下
        if (keys && keys.length > 0) {
          showKeysDialog(keys);
        } else {
          showSuccess(t('令牌创建成功，请在列表页面点击复制获取令牌！'));
        }
        props.refresh();
        props.handleClose();
      } else {
        showError(t(message));
      }

    } else {
      // 批量生成令牌（tokenCount > 1）

      let localInputs = { ...inputs };
      localInputs.remain_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_quota = parseInt(localInputs.remain_quota);
      localInputs.interval_time = parseInt(localInputs.interval_time || -1);
      localInputs.interval_unit = parseInt(localInputs.interval_unit || 3);
      localInputs.model_limits = localInputs.model_limits.join(',');
      localInputs.expired_time = -1;

      // 后端批量请求，字段 tokenCount 一起传
      let res = await API.post(
          `/api/token/tokens?tokenCount=${tokenCount}`,
          localInputs
      );
      const { success, message, keys } = res.data;
      if (success) {
        showKeysDialog(keys);
        props.refresh();
        props.handleClose();
      } else {
        showError(t(message));
      }
    }

    setLoading(false);
    formApiRef.current?.setValues(getInitValues());
  };


  return (
    <>
      <SideSheet
        placement={isEdit ? 'right' : 'left'}
        title={
          <Title level={3}>
            {isEdit ? t('更新令牌信息') : t('创建新的令牌')}
          </Title>
        }
        headerStyle={{ borderBottom: '1px solid var(--semi-color-border)' }}
        bodyStyle={{ borderBottom: '1px solid var(--semi-color-border)' }}
        visible={props.visiable}
        footer={
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <Space>
              <Button theme='solid' size={'large'} onClick={submit}>
                {t('提交')}
              </Button>
              <Button
                theme='solid'
                size={'large'}
                type={'tertiary'}
                onClick={handleCancel}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
        closeIcon={null}
        onCancel={() => handleCancel()}
        width={isMobile() ? '100%' : 600}
      >
        <Spin spinning={loading}>
          <Input
            style={{marginTop: 20}}
            label={t('名称')}
            name='name'
            placeholder={t('请输入名称')}
            onChange={(value) => handleInputChange('name', value)}
            value={name}
            autoComplete='new-password'
            required={!isEdit}
          />
          <Divider/>
          <DatePicker
            style={{display: 'none'}}
            label={t('过期时间')}
            name='expired_time'
            placeholder={t('请选择过期时间')}
            onChange={(value) => handleInputChange('expired_time', value)}
            value={expired_time}
            autoComplete='new-password'
            type='dateTime'
          />
          <div style={{marginTop: 10}}>
            <Typography.Text>{t('请选择令牌类型')}</Typography.Text>
          </div>
          <Select
            style={{marginTop: 10, width: '100%', textAlign: 'center'}}
            placeholder={t('请选择令牌类型')}
            name='interval_unit'
            onChange={(value) => handleInputChange('interval_unit', value)}
            value={interval_unit || 3}
            optionList={[
              {value: 3, label: t('天卡')},
              {value: 4, label: t('周卡')},
              {value: 5, label: t('月卡')},
              {value: 6, label: t('季卡')},
              {value: 8, label: t('周不刷新卡')},
              {value: 9, label: t('月不刷新卡')},
              {value: 10, label: t('季不刷新卡')}
            ]}
          />
          <Banner
            style={{marginTop: 10}}
            type={'warning'}
            description={t(
              '注意，令牌间隔时间与令牌类型组合使用，如一天卡、三天卡等等。不填默认为一 x 卡',
            )}
          ></Banner>
          <div style={{marginTop: 10}}>
            <Typography.Text>{t('令牌间隔时间')}</Typography.Text>
          </div>
          <Input
            style={{marginTop: 10}}
            label={t('令牌间隔')}
            name='interval_time'
            placeholder={t('请输入令牌间隔')}
            onChange={(value) => handleInputChange('interval_time', value)}
            value={interval_time}
            type='number'
          />

          <div style={{marginTop: 20, display: 'none'}}>
            <Space>
              <Button
                type={'tertiary'}
                onClick={() => {
                  setExpiredTime(0, 0, 0, 0);
                }}
              >
                {t('永不过期')}
              </Button>
              <Button
                type={'tertiary'}
                onClick={() => {
                  setExpiredTime(0, 0, 1, 0);
                }}
              >
                {t('一小时')}
              </Button>
              <Button
                type={'tertiary'}
                onClick={() => {
                  setExpiredTime(1, 0, 0, 0);
                }}
              >
                {t('一个月')}
              </Button>
              <Button
                type={'tertiary'}
                onClick={() => {
                  setExpiredTime(0, 1, 0, 0);
                }}
              >
                {t('一天')}
              </Button>
            </Space>
          </div>

          <Divider/>
          <Banner
            type={'warning'}
            description={t(
              '注意，令牌的额度仅用于限制令牌本身的最大额度使用量，实际的使用受到账户的剩余额度限制。',
            )}
          ></Banner>
          <div style={{marginTop: 20}}>
            <Typography.Text>{`${t('额度')}${renderQuotaWithPrompt(remain_quota)}`}</Typography.Text>
          </div>
          <AutoComplete
            style={{marginTop: 8}}
            name='remain_quota'
            placeholder={t('请输入额度')}
            onChange={(value) => handleInputChange('remain_quota', value)}
            value={remain_quota}
            autoComplete='new-password'
            type='number'
            // position={'top'}
            data={[
              {value: 500000, label: '1$'},
              {value: 5000000, label: '10$'},
              {value: 25000000, label: '50$'},
              {value: 50000000, label: '100$'},
              {value: 250000000, label: '500$'},
              {value: 500000000, label: '1000$'},
            ]}
            disabled={unlimited_quota}
          />

          {!isEdit && (
            <>
              <div style={{marginTop: 20}}>
                <Typography.Text>{t('新建数量')}</Typography.Text>
              </div>
              <AutoComplete
                style={{marginTop: 8}}
                label={t('数量')}
                placeholder={t('请选择或输入创建令牌的数量')}
                onChange={(value) => handleTokenCountChange(value)}
                onSelect={(value) => handleTokenCountChange(value)}
                value={tokenCount.toString()}
                autoComplete='off'
                type='number'
                data={[
                  {value: 10, label: t('10个')},
                  {value: 20, label: t('20个')},
                  {value: 30, label: t('30个')},
                  {value: 100, label: t('100个')},
                ]}
                disabled={unlimited_quota}
              />
            </>
          )}

          <div>
            <Button
              style={{marginTop: 8}}
              type={'warning'}
              onClick={() => {
                setUnlimitedQuota();
              }}
            >
              {unlimited_quota ? t('取消无限额度') : t('设为无限额度')}
            </Button>
          </div>
          <Divider/>
          <div style={{marginTop: 10}}>
            <Typography.Text>
              {t('IP白名单（请勿过度信任此功能）')}
            </Typography.Text>
          </div>
          <TextArea
            label={t('IP白名单')}
            name='allow_ips'
            placeholder={t('允许的IP，一行一个，不填写则不限制')}
            onChange={(value) => {
              handleInputChange('allow_ips', value);
            }}
            value={inputs.allow_ips}
            style={{fontFamily: 'JetBrains Mono, Consolas'}}
          />
          <div style={{marginTop: 10, display: 'flex'}}>
            <Space>
              <Checkbox
                name='model_limits_enabled'
                checked={model_limits_enabled}
                onChange={(e) =>
                  handleInputChange('model_limits_enabled', e.target.checked)
                }
              >
                {t('启用模型限制（非必要，不建议启用）')}
              </Checkbox>
            </Space>
          </div>

          <Select
            style={{marginTop: 8}}
            placeholder={t('请选择该渠道所支持的模型')}
            name='models'
            required
            multiple
            selection
            onChange={(value) => {
              handleInputChange('model_limits', value);
            }}
            value={inputs.model_limits}
            autoComplete='new-password'
            optionList={models}
            disabled={!model_limits_enabled}
          />
          <div style={{marginTop: 10}}>
            <Typography.Text>{t('令牌分组，默认为用户的分组')}</Typography.Text>
          </div>
          {groups.length > 0 ? (
            <Select
              style={{marginTop: 8}}
              placeholder={t('令牌分组，默认为用户的分组')}
              name='gruop'
              required
              selection
              onChange={(value) => {
                handleInputChange('group', value);
              }}
              position={'topLeft'}
              renderOptionItem={renderGroupOption}
              value={inputs.group}
              autoComplete='new-password'
              optionList={groups}
            />
          ) : (
            <Select
              style={{marginTop: 8}}
              placeholder={t('管理员未设置用户可选分组')}
              name='gruop'
              disabled={true}
            />
          )}
          <Divider/>

          <div style={{marginTop: 20,display: 'none'}}>
            <Typography.Text>{`${t('刷新配额')}${renderQuotaWithPrompt(interval_quota)}`}</Typography.Text>
          </div>
          <Input
            style={{marginTop: 8,display: 'none'}}
            label={t('刷新配额')}
            name='interval_quota'
            placeholder={t('请输入刷新配额')}
            onChange={(value) => handleInputChange('interval_quota', value)}
            value={interval_quota}
            type='number'
          />


        </Spin>
      </SideSheet>
    </>
  );
};

export default EditToken;
