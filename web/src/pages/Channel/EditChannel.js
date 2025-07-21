import React, { useEffect, useState, useRef, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  verifyJSON,
} from '../../helpers';
import { useIsMobile } from '../../hooks/useIsMobile.js';
import { CHANNEL_OPTIONS } from '../../constants';
import {
  SideSheet,
  Space,
  Spin,
  Button,
  Typography,
  Checkbox,
  Banner,
  Modal,
  ImagePreview,
  Card,
  Tag,
  Avatar,
  Form,
  Row,
  Col,
  Highlight,
} from '@douyinfe/semi-ui';
import { getChannelModels, copy, getChannelIcon, getModelCategories } from '../../helpers';
import {
  IconSave,
  IconClose,
  IconServer,
  IconSetting,
  IconCode,
  IconGlobe,
  IconBolt,
} from '@douyinfe/semi-icons';

const { Text, Title } = Typography;
import { getChannelModels, loadChannelModels } from '../../components/utils.js';
import { IconHelpCircle } from '@douyinfe/semi-icons';
import axios from 'axios';
import { Switch } from 'antd';


const MODEL_MAPPING_EXAMPLE = {
  'gpt-3.5-turbo': 'gpt-3.5-turbo-0125',
};

const STATUS_CODE_MAPPING_EXAMPLE = {
  400: '500',
};

const REGION_EXAMPLE = {
  default: 'us-central1',
  'claude-3-5-sonnet-20240620': 'europe-west1',
};

function type2secretPrompt(type) {
  // inputs.type === 15 ? '按照如下格式输入：APIKey|SecretKey' : (inputs.type === 18 ? '按照如下格式输入：APPID|APISecret|APIKey' : '请输入渠道对应的鉴权密钥')
  switch (type) {
    case 15:
      return '按照如下格式输入：APIKey|SecretKey';
    case 18:
      return '按照如下格式输入：APPID|APISecret|APIKey';
    case 22:
      return '按照如下格式输入：APIKey-AppId，例如：fastgpt-0sp2gtvfdgyi4k30jwlgwf1i-64f335d84283f05518e9e041';
    case 23:
      return '按照如下格式输入：AppId|SecretId|SecretKey';
    case 33:
      return '按照如下格式输入：Ak|Sk|Region';
    case 50:
      return '按照如下格式输入: AccessKey|SecretKey';
    case 51:
      return '按照如下格式输入: Access Key ID|Secret Access Key';
    default:
      return '请输入渠道对应的鉴权密钥';
  }
}

const EditChannel = (props) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const channelId = props.editingChannel.id;
  const isEdit = channelId !== undefined;
  const [loading, setLoading] = useState(isEdit);
  const isMobile = useIsMobile();
  const [billingSupplementList, setBillingSupplementList] = useState([]);

  const handleCancel = () => {
    props.handleClose();
  };
  const originInputs = {
    name: '',
    type: 1,
    key: '',
    openai_organization: '',
    max_input_tokens: 0,
    base_url: '',
    other: '',
    model_mapping: '',
    status_code_mapping: '',
    models: [],
    auto_ban: 1,
    test_model: '',
    groups: ['default'],
    priority: 0,
    weight: 0,
    tag: '',
    is_convert_role: 2,
    audit_enabled: 0,
    audit_categories: "",
    audit_apikey: "",
    audit_url: "",
    audit_model: "",
    billing_supplement: "",

    multi_key_mode: 'random',
  };
  const [batch, setBatch] = useState(false);
  const [multiToSingle, setMultiToSingle] = useState(false);
  const [multiKeyMode, setMultiKeyMode] = useState('random');
  const [autoBan, setAutoBan] = useState(true);
  const [inputs, setInputs] = useState(originInputs);
  const [originModelOptions, setOriginModelOptions] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);
  const [basicModels, setBasicModels] = useState([]);
  const [fullModels, setFullModels] = useState([]);
  const [customModel, setCustomModel] = useState('');
  const [modalImageUrl, setModalImageUrl] = useState('');
  const [isModalOpenurl, setIsModalOpenurl] = useState(false);
  const formApiRef = useRef(null);
  const [vertexKeys, setVertexKeys] = useState([]);
  const [vertexFileList, setVertexFileList] = useState([]);
  const vertexErroredNames = useRef(new Set()); // 避免重复报错
  const [isMultiKeyChannel, setIsMultiKeyChannel] = useState(false);
  const [channelSearchValue, setChannelSearchValue] = useState('');
  const [useManualInput, setUseManualInput] = useState(false); // 是否使用手动输入模式
  const getInitValues = () => ({ ...originInputs });
  const handleInputChange = (name, value) => {
    if (formApiRef.current) {
      formApiRef.current.setValue(name, value);
    }
    if (name === 'models' && Array.isArray(value)) {
      value = Array.from(new Set(value.map((m) => (m || '').trim())));
    }

    if (name === 'base_url' && value.endsWith('/v1')) {
      Modal.confirm({
        title: '警告',
        content:
          '不需要在末尾加/v1，New API会自动处理，添加后可能导致请求失败，是否继续？',
        onOk: () => {
          setInputs((inputs) => ({ ...inputs, [name]: value }));
        },
      });
      return;
    }
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (name === 'type') {
      let localModels = [];
      switch (value) {
        case 2:
          localModels = [
            'mj_imagine',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_uploads',
          ];
          break;
        case 5:
          localModels = [
            'swap_face',
            'mj_imagine',
            'mj_video',
            'mj_edits',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_zoom',
            'mj_shorten',
            'mj_modal',
            'mj_inpaint',
            'mj_custom_zoom',
            'mj_high_variation',
            'mj_low_variation',
            'mj_pan',
            'mj_uploads',
          ];
          break;
        case 36:
          localModels = ['suno_music', 'suno_lyrics'];
          break;
        default:
          localModels = getChannelModels(value);
          break;
      }
      if (inputs.models.length === 0) {
        setInputs((inputs) => ({ ...inputs, models: localModels }));
      }
      setBasicModels(localModels);

      // 重置手动输入模式状态
      setUseManualInput(false);
    }
    //setAutoBan
  };

  const loadChannel = async () => {
    setLoading(true);
    let res = await API.get(`/api/channel/${channelId}`);
    if (res === undefined) {
      return;
    }
    const { success, message, data } = res.data;
    if (success) {
      if (data.models === '') {
        data.models = [];
      } else {
        data.models = data.models.split(',');
      }
      if (data.group === '') {
        data.groups = [];
      } else {
        data.groups = data.group.split(',');
      }
      if (data.model_mapping !== '') {
        data.model_mapping = JSON.stringify(
          JSON.parse(data.model_mapping),
          null,
          2,
        );
      }
      const chInfo = data.channel_info || {};
      const isMulti = chInfo.is_multi_key === true;
      setIsMultiKeyChannel(isMulti);
      if (isMulti) {
        setBatch(true);
        setMultiToSingle(true);
        const modeVal = chInfo.multi_key_mode || 'random';
        setMultiKeyMode(modeVal);
        data.multi_key_mode = modeVal;
      } else {
        setBatch(false);
        setMultiToSingle(false);
      }
      setInputs(data);
      if (formApiRef.current) {
        formApiRef.current.setValues(data);
      }
      if (data.auto_ban === 0) {
        setAutoBan(false);
      } else {
        setAutoBan(true);
      }
      if (data.billing_supplement) {
        try {
          setBillingSupplementList(JSON.parse(data.billing_supplement));
        } catch (e) {
          setBillingSupplementList([]);
        }
      } else {
        setBillingSupplementList([]);
      }
      setBasicModels(getChannelModels(data.type));
      // console.log(data);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const fetchUpstreamModelList = async (name) => {
    // if (inputs['type'] !== 1) {
    //   showError(t('仅支持 OpenAI 接口格式'));
    //   return;
    // }
    setLoading(true);
    const models = inputs['models'] || [];
    let err = false;

    if (isEdit) {
      // 如果是编辑模式，使用已有的 channelId 获取模型列表
      const res = await API.get('/api/channel/fetch_models/' + channelId, { skipErrorHandler: true });
      if (res && res.data && res.data.success) {
        models.push(...res.data.data);
      } else {
        err = true;
      }
    } else {
      // 如果是新建模式，通过后端代理获取模型列表
      if (!inputs?.['key']) {
        showError(t('请填写密钥'));
        err = true;
      } else {
        try {
          const res = await API.post(
            '/api/channel/fetch_models',
            {
              base_url: inputs['base_url'],
              type: inputs['type'],
              key: inputs['key'],
            },
            { skipErrorHandler: true },
          );

          if (res && res.data && res.data.success) {
            models.push(...res.data.data);
          } else {
            err = true;
          }
        } catch (error) {
          console.error('Error fetching models:', error);
          err = true;
        }
      }
    }

    if (!err) {
      handleInputChange(name, Array.from(new Set(models)));
      showSuccess(t('获取模型列表成功'));
    } else {
      showError(t('获取模型列表失败'));
    }
    setLoading(false);
  };

  const fetchModels = async () => {
    try {
      let res = await API.get(`/api/channel/models`);
      const localModelOptions = res.data.data.map((model) => {
        const id = (model.id || '').trim();
        return {
          key: id,
          label: id,
          value: id,
        };
      });
      setOriginModelOptions(localModelOptions);
      setFullModels(res.data.data.map((model) => model.id));
      setBasicModels(
        res.data.data
          .filter((model) => {
            return model.id.startsWith('gpt-') || model.id.startsWith('text-');
          })
          .map((model) => model.id),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res === undefined) {
        return;
      }
      setGroupOptions(
        res.data.data.map((group) => ({
          label: group,
          value: group,
        })),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  useEffect(() => {
    const modelMap = new Map();

    originModelOptions.forEach((option) => {
      const v = (option.value || '').trim();
      if (!modelMap.has(v)) {
        modelMap.set(v, option);
      }
    });

    inputs.models.forEach((model) => {
      const v = (model || '').trim();
      if (!modelMap.has(v)) {
        modelMap.set(v, {
          key: v,
          label: v,
          value: v,
        });
      }
    });

    const categories = getModelCategories(t);
    const optionsWithIcon = Array.from(modelMap.values()).map((opt) => {
      const modelName = opt.value;
      let icon = null;
      for (const [key, category] of Object.entries(categories)) {
        if (key !== 'all' && category.filter({ model_name: modelName })) {
          icon = category.icon;
          break;
        }
      }
      return {
        ...opt,
        label: (
          <span className="flex items-center gap-1">
            {icon}
            {modelName}
          </span>
        ),
      };
    });

    setModelOptions(optionsWithIcon);
  }, [originModelOptions, inputs.models, t]);

  useEffect(() => {
    fetchModels().then();
    fetchGroups().then();
    if (isEdit) {
      loadChannel().then(() => {
        // 处理已有的审核类别
        if (data.audit_categories) {
          try {
            // 尝试作为JSON解析
            JSON.parse(data.audit_categories);
          } catch (e) {
            // 如果不是有效的JSON，则转换旧格式
            const oldCategories = data.audit_categories.split(',').map(item => item.trim()).filter(item => item);
            // 转换为新格式 ["类别:0.9", ...]
            const newCategories = oldCategories.map(cat => `${cat}:0.9`);
            handleInputChange('audit_categories', JSON.stringify(newCategories));
          }
        } else {
          // 设置为空数组
          handleInputChange('audit_categories', '[]');
        }
      });
      if (formApiRef.current) {
        formApiRef.current.setValues(originInputs);
      }
    } else {
      setInputs(originInputs);
      handleInputChange('audit_categories', '[]');
      let localModels = getChannelModels(inputs.type);
      setBasicModels(localModels);
      setInputs((inputs) => ({ ...inputs, models: localModels }));
    }
  }, [props.editingChannel.id]);



  useEffect(() => {
    if (formApiRef.current) {
      formApiRef.current.setValues(inputs);
    }
  }, [inputs]);

  useEffect(() => {
    if (props.visible) {
      if (isEdit) {
        loadChannel();
      } else {
        formApiRef.current?.setValues(getInitValues());
      }
      // 重置手动输入模式状态
      setUseManualInput(false);
    } else {
      formApiRef.current?.reset();
    }
  }, [props.visible, channelId]);

  const handleVertexUploadChange = ({ fileList }) => {
    vertexErroredNames.current.clear();
    (async () => {
      let validFiles = [];
      let keys = [];
      const errorNames = [];
      for (const item of fileList) {
        const fileObj = item.fileInstance;
        if (!fileObj) continue;
        try {
          const txt = await fileObj.text();
          keys.push(JSON.parse(txt));
          validFiles.push(item);
        } catch (err) {
          if (!vertexErroredNames.current.has(item.name)) {
            errorNames.push(item.name);
            vertexErroredNames.current.add(item.name);
          }
        }
      }

      // 非批量模式下只保留一个文件（最新选择的），避免重复叠加
      if (!batch && validFiles.length > 1) {
        validFiles = [validFiles[validFiles.length - 1]];
        keys = [keys[keys.length - 1]];
      }

      setVertexKeys(keys);
      setVertexFileList(validFiles);
      if (formApiRef.current) {
        formApiRef.current.setValue('vertex_files', validFiles);
      }
      setInputs((prev) => ({ ...prev, vertex_files: validFiles }));

      if (errorNames.length > 0) {
        showError(t('以下文件解析失败，已忽略：{{list}}', { list: errorNames.join(', ') }));
      }
    })();
  };

  const submit = async () => {
    const formValues = formApiRef.current ? formApiRef.current.getValues() : {};
    let localInputs = { ...formValues };

    if (localInputs.type === 41) {
      if (useManualInput) {
        // 手动输入模式
        if (localInputs.key && localInputs.key.trim() !== '') {
          try {
            // 验证 JSON 格式
            const parsedKey = JSON.parse(localInputs.key);
            // 确保是有效的密钥格式
            localInputs.key = JSON.stringify(parsedKey);
          } catch (err) {
            showError(t('密钥格式无效，请输入有效的 JSON 格式密钥'));
            return;
          }
        } else if (!isEdit) {
          showInfo(t('请输入密钥！'));
          return;
        }
      } else {
        // 文件上传模式
        let keys = vertexKeys;

        // 若当前未选择文件，尝试从已上传文件列表解析（异步读取）
        if (keys.length === 0 && vertexFileList.length > 0) {
          try {
            const parsed = await Promise.all(
              vertexFileList.map(async (item) => {
                const fileObj = item.fileInstance;
                if (!fileObj) return null;
                const txt = await fileObj.text();
                return JSON.parse(txt);
              })
            );
            keys = parsed.filter(Boolean);
          } catch (err) {
            showError(t('解析密钥文件失败: {{msg}}', { msg: err.message }));
            return;
          }
        }

        // 创建模式必须上传密钥；编辑模式可选
        if (keys.length === 0) {
          if (!isEdit) {
            showInfo(t('请上传密钥文件！'));
            return;
          } else {
            // 编辑模式且未上传新密钥，不修改 key
            delete localInputs.key;
          }
        } else {
          // 有新密钥，则覆盖
          if (batch) {
            localInputs.key = JSON.stringify(keys);
          } else {
            localInputs.key = JSON.stringify(keys[0]);
          }
        }
      }
    }

    // 如果是编辑模式且 key 为空字符串，避免提交空值覆盖旧密钥
    if (isEdit && (!localInputs.key || localInputs.key.trim() === '')) {
      delete localInputs.key;
    }
    delete localInputs.vertex_files;

    if (!isEdit && (!localInputs.name || !localInputs.key)) {
      showInfo(t('请填写渠道名称和渠道密钥！'));
      return;
    }
    if (!Array.isArray(localInputs.models) || localInputs.models.length === 0) {
      showInfo(t('请至少选择一个模型！'));
      return;
    }
    if (localInputs.model_mapping && localInputs.model_mapping !== '' && !verifyJSON(localInputs.model_mapping)) {
      showInfo(t('模型映射必须是合法的 JSON 格式！'));
      return;
    }
    if (localInputs.base_url && localInputs.base_url.endsWith('/')) {
      localInputs.base_url = localInputs.base_url.slice(
        0,
        localInputs.base_url.length - 1,
      );
    }
    if (localInputs.type === 18 && localInputs.other === '') {
      localInputs.other = 'v2.1';
    }
    // 处理审核类别
    if (localInputs.audit_enabled === 1 && localInputs.audit_categories) {
      try {
        // 确保是有效的JSON格式
        const categories = JSON.parse(localInputs.audit_categories);
        // 保持JSON字符串格式
        localInputs.audit_categories = JSON.stringify(categories);
      } catch (e) {
        // 如果解析出错，设置为空数组
        localInputs.audit_categories = '[]';
      }
    }


    let res;
    localInputs.auto_ban = localInputs.auto_ban ? 1 : 0;
    localInputs.models = localInputs.models.join(',');
    localInputs.group = (localInputs.groups || []).join(',');

    let mode = 'single';
    if (batch) {
      mode = multiToSingle ? 'multi_to_single' : 'batch';
    }

    if (isEdit) {
      res = await API.put(`/api/channel/`, {
        ...localInputs,
        id: parseInt(channelId),
      });
    } else {
      res = await API.post(`/api/channel/`, {
        mode: mode,
        multi_key_mode: mode === 'multi_to_single' ? multiKeyMode : undefined,
        channel: localInputs,
      });
    }
    const { success, message } = res.data;
    if (success) {
      if (isEdit) {
        showSuccess(t('渠道更新成功！'));
      } else {
        showSuccess(t('渠道创建成功！'));
        setInputs(originInputs);
      }
      props.refresh();
      props.handleClose();
    } else {
      showError(message);
    }
  };

  const addCustomModels = () => {
    if (customModel.trim() === '') return;
    const modelArray = customModel.split(',').map((model) => model.trim());

    let localModels = [...inputs.models];
    let localModelOptions = [...modelOptions];
    const addedModels = [];

    modelArray.forEach((model) => {
      if (model && !localModels.includes(model)) {
        localModels.push(model);
        localModelOptions.push({
          key: model,
          label: model,
          value: model,
        });
        addedModels.push(model);
      }
    });

    setModelOptions(localModelOptions);
    setCustomModel('');
    handleInputChange('models', localModels);

    if (addedModels.length > 0) {
      showSuccess(
        t('已新增 {{count}} 个模型：{{list}}', {
          count: addedModels.length,
          list: addedModels.join(', '),
        })
      );
    } else {
      showInfo(t('未发现新增模型'));
    }
  };

  const batchAllowed = !isEdit || isMultiKeyChannel;
  const batchExtra = batchAllowed ? (
    <Space>
      <Checkbox
        disabled={isEdit}
        checked={batch}
        onChange={(e) => {
          const checked = e.target.checked;

          if (!checked && vertexFileList.length > 1) {
            Modal.confirm({
              title: t('切换为单密钥模式'),
              content: t('将仅保留第一个密钥文件，其余文件将被移除，是否继续？'),
              onOk: () => {
                const firstFile = vertexFileList[0];
                const firstKey = vertexKeys[0] ? [vertexKeys[0]] : [];

                setVertexFileList([firstFile]);
                setVertexKeys(firstKey);

                formApiRef.current?.setValue('vertex_files', [firstFile]);
                setInputs((prev) => ({ ...prev, vertex_files: [firstFile] }));

                setBatch(false);
                setMultiToSingle(false);
                setMultiKeyMode('random');
              },
              onCancel: () => {
                setBatch(true);
              },
              centered: true,
            });
            return;
          }

          setBatch(checked);
          if (!checked) {
            setMultiToSingle(false);
            setMultiKeyMode('random');
          } else {
            // 批量模式下禁用手动输入，并清空手动输入的内容
            setUseManualInput(false);
            if (inputs.type === 41) {
              // 清空手动输入的密钥内容
              if (formApiRef.current) {
                formApiRef.current.setValue('key', '');
              }
              handleInputChange('key', '');
            }
          }
        }}
      >{t('批量创建')}</Checkbox>
      {/*{batch && (*/}
      {/*  <Checkbox disabled={isEdit} checked={multiToSingle} onChange={() => {*/}
      {/*    setMultiToSingle(prev => !prev);*/}
      {/*    setInputs(prev => {*/}
      {/*      const newInputs = { ...prev };*/}
      {/*      if (!multiToSingle) {*/}
      {/*        newInputs.multi_key_mode = multiKeyMode;*/}
      {/*      } else {*/}
      {/*        delete newInputs.multi_key_mode;*/}
      {/*      }*/}
      {/*      return newInputs;*/}
      {/*    });*/}
      {/*  }}>{t('密钥聚合模式')}</Checkbox>*/}
      {/*)}*/}
    </Space>
  ) : null;

  const channelOptionList = useMemo(
    () =>
      CHANNEL_OPTIONS.map((opt) => ({
        ...opt,
        // 保持 label 为纯文本以支持搜索
        label: opt.label,
      })),
    [],
  );

  const renderChannelOption = (renderProps) => {
    const {
      disabled,
      selected,
      label,
      value,
      focused,
      className,
      style,
      onMouseEnter,
      onClick,
      ...rest
    } = renderProps;

    const searchWords = channelSearchValue ? [channelSearchValue] : [];

    // 构建样式类名
    const optionClassName = [
      'flex items-center gap-3 px-3 py-2 transition-all duration-200 rounded-lg mx-2 my-1',
      focused && 'bg-blue-50 shadow-sm',
      selected && 'bg-blue-100 text-blue-700 shadow-lg ring-2 ring-blue-200 ring-opacity-50',
      disabled && 'opacity-50 cursor-not-allowed',
      !disabled && 'hover:bg-gray-50 hover:shadow-md cursor-pointer',
      className
    ].filter(Boolean).join(' ');

    return (
      <div
        style={style}
        className={optionClassName}
        onClick={() => !disabled && onClick()}
        onMouseEnter={e => onMouseEnter()}
      >
        <div className="flex items-center gap-3 w-full">
          <div className="flex-shrink-0 w-5 h-5 flex items-center justify-center">
            {getChannelIcon(value)}
          </div>
          <div className="flex-1 min-w-0">
            <Highlight
              sourceString={label}
              searchWords={searchWords}
              className="text-sm font-medium truncate"
            />
          </div>
          {selected && (
            <div className="flex-shrink-0 text-blue-600">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
                <path d="M13.78 4.22a.75.75 0 010 1.06l-7.25 7.25a.75.75 0 01-1.06 0L2.22 9.28a.75.75 0 011.06-1.06L6 10.94l6.72-6.72a.75.75 0 011.06 0z"/>
              </svg>
            </div>
          )}
        </div>
      </div>
    );
  };

  return (
    <>
      <SideSheet
        placement={isEdit ? 'right' : 'left'}
        title={
          <Space>
            <Tag color="blue" shape="circle">{isEdit ? t('编辑') : t('新建')}</Tag>
            <Title heading={4} className="m-0">
              {isEdit ? t('更新渠道信息') : t('创建新的渠道')}
            </Title>
          </Space>
        }
        bodyStyle={{ padding: '0' }}
        visible={props.visible}
        width={isMobile ? '100%' : 600}
        footer={
          <div className="flex justify-end bg-white">
            <Space>
              <Button
                theme="solid"
                onClick={() => formApiRef.current?.submitForm()}
                icon={<IconSave />}
              >
                {t('提交')}
              </Button>
              <Button
                theme="light"
                type="primary"
                onClick={handleCancel}
                icon={<IconClose />}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
        closeIcon={null}
        onCancel={() => handleCancel()}
      >
        <Spin spinning={loading}>
          <div style={{marginTop: 10}}>

            <Typography.Text strong>{t('类型')}：</Typography.Text>
          </div>
          <Select
            name='type'
            required
            optionList={CHANNEL_OPTIONS}
            value={inputs.type}
            onChange={(value) => handleInputChange('type', value)}
            style={{width: '50%'}}
            filter
            searchPosition='dropdown'
            placeholder={t('请选择渠道类型')}
          />
          {inputs.type === 40 && (
            <div style={{marginTop: 10}}>
              <Banner
                type='info'
                description={
                  <div>
                    <Typography.Text strong>{t('邀请链接')}:</Typography.Text>
                    <Typography.Text
                      link
                      underline
                      style={{marginLeft: 8}}
                      onClick={() =>
                        window.open('https://cloud.siliconflow.cn/i/hij0YNTZ')
                      }
                    >
                      https://cloud.siliconflow.cn/i/hij0YNTZ
                    </Typography.Text>
                  </div>
                }
              />
            </div>
          )}
          {inputs.type === 3 && (
            <>
              <div style={{ marginTop: 10 }}>
                <Banner
                  type={'warning'}
                  description={
                    <>
                      {t(
                        '2025年5月10日后添加的渠道，不需要再在部署的时候移除模型名称中的"."',
                      )}
                      {/*<br />*/}
                      {/*<Typography.Text*/}
                      {/*  style={{*/}
                      {/*    color: 'rgba(var(--semi-blue-5), 1)',*/}
                      {/*    userSelect: 'none',*/}
                      {/*    cursor: 'pointer',*/}
                      {/*  }}*/}
                      {/*  onClick={() => {*/}
                      {/*    setModalImageUrl(*/}
                      {/*      '/azure_model_name.png',*/}
                      {/*    );*/}
                      {/*    setIsModalOpenurl(true)*/}

                      {/*  }}*/}
                      {/*>*/}
                      {/*  {t('查看示例')}*/}
                      {/*</Typography.Text>*/}
                    </>
                  }
                ></Banner>
              </div>
              <div style={{ marginTop: 10 }}>
                <Typography.Text strong>
                  AZURE_OPENAI_ENDPOINT：
                </Typography.Text>
              </div>
              <Input
                label='AZURE_OPENAI_ENDPOINT'
                name='azure_base_url'
                placeholder={t(
                  '请输入 AZURE_OPENAI_ENDPOINT，例如：https://docs-test-001.openai.azure.com',
                )}
                onChange={(value) => {
                  handleInputChange('base_url', value);
                }}
                value={inputs.base_url}
                autoComplete='new-password'
              />
              <div style={{ marginTop: 10 }}>
                <Typography.Text strong>{t('默认 API 版本')}：</Typography.Text>
              </div>
              <Input
                label={t('默认 API 版本')}
                name='azure_other'
                placeholder={t('请输入默认 API 版本，例如：2025-04-01-preview')}
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 8 && (
            <>
              <div style={{marginTop: 10}}>
                <Banner
                  type={'warning'}
                  description={t(
                    '如果你对接的是上游One API或者New API等转发项目，请使用OpenAI类型，不要使用此类型，除非你知道你在做什么。',
                  )}
                ></Banner>
              </div>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>
                  {t('完整的 Base URL，支持变量{model}')}：
                </Typography.Text>
              </div>
              <Input
                name='base_url'
                placeholder={t(
                  '请输入完整的URL，例如：https://api.openai.com/v1/chat/completions',
                )}
                onChange={(value) => {
                  handleInputChange('base_url', value);
                }}
                value={inputs.base_url}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 37 && (
            <>
              <div style={{marginTop: 10}}>
                <Banner
                  type={'warning'}
                  description={t(
                    'Dify渠道只适配chatflow和agent，并且agent不支持图片！',
                  )}
                ></Banner>
              </div>
            </>
          )}
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('名称')}：</Typography.Text>
          </div>
          <Input
            required
            name='name'
            placeholder={t('请为渠道命名')}
            onChange={(value) => {
              handleInputChange('name', value);
            }}
            value={inputs.name}
            autoComplete='new-password'
          />
          {inputs.type !== 3 &&
            inputs.type !== 8 &&
            inputs.type !== 22 &&
            inputs.type !== 36 &&
            inputs.type !== 45 && (
              <>
                <div style={{ marginTop: 10 }}>
                  <Typography.Text strong>{t('API地址')}：</Typography.Text>
                </div>
                <Tooltip
                  content={t(
                    '对于官方渠道，new-api已经内置地址，除非是第三方代理站点或者Azure的特殊接入地址，否则不需要填写',
                  )}
                >
                  <Input
                    label={t('API地址')}
                    name='base_url'
                    placeholder={t(
                      '此项可选，用于通过自定义API地址来进行 API 调用，末尾不要带/v1和/',
                    )}
                    onChange={(value) => {
                      handleInputChange('base_url', value);
                    }}
                    value={inputs.base_url}
                    autoComplete='new-password'
                  />
                </Tooltip>
              </>
            )}
          <div style={{ marginTop: 10 }}>
            <Typography.Text strong>{t('密钥')}：</Typography.Text>
          </div>
          {batch ? (
            <TextArea
              label={t('密钥')}
              name='key'
              required
              placeholder={t('请输入密钥，一行一个')}
              onChange={(value) => {
                handleInputChange('key', value);
              }}
              value={inputs.key}
              style={{minHeight: 150, fontFamily: 'JetBrains Mono, Consolas'}}
              autoComplete='new-password'
            />
          ) : (
            <>
              {inputs.type === 41 ? (
                <TextArea
                  label={t('鉴权json')}
                  name='key'
                  required
                  placeholder={
                    '{\n' +
                    '  "type": "service_account",\n' +
                    '  "project_id": "abc-bcd-123-456",\n' +
                    '  "private_key_id": "123xxxxx456",\n' +
                    '  "private_key": "-----BEGIN PRIVATE KEY-----xxxx\n' +
                    '  "client_email": "xxx@developer.gserviceaccount.com",\n' +
                    '  "client_id": "111222333",\n' +
                    '  "auth_uri": "https://accounts.google.com/o/oauth2/auth",\n' +
                    '  "token_uri": "https://oauth2.googleapis.com/token",\n' +
                    '  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",\n' +
                    '  "client_x509_cert_url": "https://xxxxx.gserviceaccount.com",\n' +
                    '  "universe_domain": "googleapis.com"\n' +
                    '}'
                  }
                  onChange={(value) => {
                    handleInputChange('key', value);
                  }}
                  autosize={{minRows: 10}}
                  value={inputs.key}
                  autoComplete='new-password'
                />
              ) : (
                <Input
                  label={t('密钥')}
                  name='key'
                  required
                  placeholder={t(type2secretPrompt(inputs.type))}
                  onChange={(value) => {
                    handleInputChange('key', value);
                  }}
                  value={inputs.key}
                  autoComplete='new-password'
                />
              )}
            </>
          )}
          {!isEdit && (
            <div style={{marginTop: 10, display: 'flex'}}>
              <Space>
                <Checkbox
                  checked={batch}
                  label={t('批量创建')}
                  name='batch'
                  onChange={() => setBatch(!batch)}
                />
                <Typography.Text strong>{t('批量创建')}</Typography.Text>
              </Space>
            </div>
          )}
          {inputs.type === 22 && (
            <>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>{t('私有部署地址')}：</Typography.Text>
              </div>
              <Input
                name='base_url'
                placeholder={t(
                  '请输入私有部署地址，格式为：https://fastgpt.run/api/openapi',
                )}
                onChange={(value) => {
                  handleInputChange('base_url', value);
                }}
                value={inputs.base_url}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 36 && (
            <>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>
                  {t(
                    '注意非Chat API，请务必填写正确的API地址，否则可能导致无法使用',
                  )}
                </Typography.Text>
              </div>
              <Input
                name='base_url'
                placeholder={t(
                  '请输入到 /suno 前的路径，通常就是域名，例如：https://api.example.com',
                )}
                onChange={(value) => {
                  handleInputChange('base_url', value);
                }}
                value={inputs.base_url}
                autoComplete='new-password'
              />
            </>
          )}
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('分组')}：</Typography.Text>
          </div>
          <Select
            placeholder={t('请选择可以使用该渠道的分组')}
            name='groups'
            required
            multiple
            selection
            allowAdditions
            additionLabel={t('请在系统设置页面编辑分组倍率以添加新的分组：')}
            onChange={(value) => {
              handleInputChange('groups', value);
            }}
            value={inputs.groups}
            autoComplete='new-password'
            optionList={groupOptions}
          />
          {inputs.type === 18 && (
            <>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>模型版本：</Typography.Text>
              </div>
              <Input
                name='other'
                placeholder={
                  '请输入星火大模型版本，注意是接口地址中的版本号，例如：v2.1'
                }
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 41 && (
            <>
              <div style={{ marginTop: 10 }}>
                <Typography.Text strong>{t('部署地区')}：</Typography.Text>
              </div>
              <TextArea
                name='other'
                placeholder={t(
                  '请输入部署地区，例如：us-central1\n支持使用模型映射格式\n' +
                  '{\n' +
                  '    "default": "us-central1",\n' +
                  '    "claude-3-5-sonnet-20240620": "europe-west1"\n' +
                  '}',
                )}
                autosize={{ minRows: 2 }}
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
              <Typography.Text
                style={{
                  color: 'rgba(var(--semi-blue-5), 1)',
                  userSelect: 'none',
                  cursor: 'pointer',
                }}
                onClick={() => {
                  handleInputChange(
                    'other',
                    JSON.stringify(REGION_EXAMPLE, null, 2),
                  );
                }}
              >
                {t('填入模板')}
              </Typography.Text>
            </>
          )}
          {inputs.type === 21 && (
            <>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>知识库 ID：</Typography.Text>
              </div>
              <Input
                label='知识库 ID'
                name='other'
                placeholder={'请输入知识库 ID，例如：123456'}
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 39 && (
            <>
              <div style={{ marginTop: 10 }}>
                <Typography.Text strong>Account ID：</Typography.Text>
              </div>
              <Input
                name='other'
                placeholder={
                  '请输入Account ID，例如：d6b5da8hk1awo8nap34ube6gh'
                }
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
            </>
          )}
          {inputs.type === 49 && (
            <>
              <div style={{ marginTop: 10 }}>
                <Typography.Text strong>智能体ID：</Typography.Text>
              </div>
              <Input
                name='other'
                placeholder={'请输入智能体ID，例如：7342866812345'}
                onChange={(value) => {
                  handleInputChange('other', value);
                }}
                value={inputs.other}
                autoComplete='new-password'
              />
            </>
          )}
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('模型')}：</Typography.Text>
          </div>
          <Select
            placeholder={'请选择该渠道所支持的模型'}
            name='models'
            required
            multiple
            selection
            filter
            searchPosition='dropdown'
            onChange={(value) => {
              handleInputChange('models', value);
            }}
            value={inputs.models}
            autoComplete='new-password'
            optionList={modelOptions}
          />
          <div style={{lineHeight: '40px', marginBottom: '12px'}}>
            <Space>
              <Button
                type='primary'
                onClick={() => {
                  handleInputChange('models', basicModels);
                }}
              >
                {t('填入相关模型')}
              </Button>
              <Button
                type='secondary'
                onClick={() => {
                  handleInputChange('models', fullModels);
                }}
              >
                {t('填入所有模型')}
              </Button>
              <Tooltip
                content={t(
                  '新建渠道时，请求通过当前浏览器发出；编辑已有渠道，请求通过后端服务器发出',
                )}
              >
                <Button
                  type='tertiary'
                  onClick={() => {
                    fetchUpstreamModelList('models');
                  }}
                >
                  {t('获取模型列表')}
                </Button>
              </Tooltip>
              <Button
                type='warning'
                onClick={() => {
                  handleInputChange('models', []);
                }}
              >
                {t('清除所有模型')}
              </Button>
            </Space>
            <Input
              addonAfter={
                <Button type='primary' onClick={addCustomModels}>
                  {t('填入')}
                </Button>
              }
              placeholder={t('输入自定义模型名称')}
              value={customModel}
              onChange={(value) => {
                setCustomModel(value.trim());
              }}
            />
          </div>
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('模型重定向')}：</Typography.Text>
          </div>
          <TextArea
            placeholder={
              t(
                '此项可选，用于修改请求体中的模型名称，为一个 JSON 字符串，键为请求中模型名称，值为要替换的模型名称，例如：',
              ) + `\n${JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2)}`
            }
            name='model_mapping'
            onChange={(value) => {
              handleInputChange('model_mapping', value);
            }}
            autosize
            value={inputs.model_mapping}
            autoComplete='new-password'
          />
          <Typography.Text
            style={{
              color: 'rgba(var(--semi-blue-5), 1)',
              userSelect: 'none',
              cursor: 'pointer',
            }}
            onClick={() => {
              handleInputChange(
                'model_mapping',
                JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2),
              );
            }}
          >
            {t('填入模板')}
          </Typography.Text>
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('渠道标签')}</Typography.Text>
          </div>
          <Input
            label={t('渠道标签')}
            name='tag'
            placeholder={t('渠道标签')}
            onChange={(value) => {
              handleInputChange('tag', value);
            }}
            value={inputs.tag}
            autoComplete='new-password'
          />

          <div style={{marginTop: 10}}>
            <Typography.Text strong>
              {t('是否转换角色 assistant 为 user')}
            </Typography.Text>
          </div>
          <Switch
            checked={inputs.is_convert_role === 1}
            onChange={(checked) => handleInputChange('is_convert_role', checked ? 1 : 2)}
            checkedChildren={t('是')}
            unCheckedChildren={t('否')}
          />


          {/* ---- 审核功能区 ---- */}
          <div style={{marginTop: 10}}>
            <Space align="center">
              <Typography.Text strong>是否开启内容审核：</Typography.Text>
              <Switch
                checked={inputs.audit_enabled === 1}
                onChange={(checked) =>
                  handleInputChange("audit_enabled", checked ? 1 : 0)
                }
                checkedChildren="开"
                unCheckedChildren="关"
              />
            </Space>
          </div>
          {inputs.audit_enabled === 1 && (
            <div style={{border: "1px solid #eee", padding: 12, borderRadius: 6, marginTop: 12}}>
              {/* 选择审核项 */}
              <div style={{marginBottom: 10}}>
                <Typography.Text strong>
                  审核类别及阈值
                  <Typography.Text type="secondary" style={{fontSize: 12, marginLeft: 6}}>
                    （格式：类别:阈值，如hate:0.9）
                  </Typography.Text>
                </Typography.Text>
                <div>
                  {/* 审核类别列表 */}
                  {inputs.audit_categories ?
                    JSON.parse(inputs.audit_categories || '[]').map((item, index) => {
                      const [category, threshold] = item.split(':');

                      // 获取所有已选类别，除了当前项
                      const selectedCategories = JSON.parse(inputs.audit_categories || '[]')
                        .filter((_, i) => i !== index)
                        .map(item => item.split(':')[0]);

                      // 过滤掉已选择的类别
                      const availableOptions = Object.entries({
                        "harassment": "骚扰言论",
                        "harassment/threatening": "威胁性骚扰",
                        "hate": "仇恨言论",
                        "hate/threatening": "威胁性仇恨",
                        "illicit": "非法行为指导",
                        "illicit/violent": "暴力违法指导",
                        "self-harm": "自残倾向",
                        "self-harm/instructions": "自残实施指导",
                        "self-harm/intent": "自残意图表露",
                        "sexual": "性内容",
                        "sexual/minors": "未成年人涉性",
                        "violence": "暴力内容",
                        "violence/graphic": "血腥暴力内容"
                      }).filter(([value, _]) => !selectedCategories.includes(value) || value === category)
                        .map(([value, label]) => ({
                          value,
                          label: `${value} - ${label}`,
                        }));

                      return (
                        <div key={index} style={{display: 'flex', alignItems: 'center', gap: 8, marginTop: 8}}>
                          <Select
                            style={{width: 240}}
                            value={category}
                            onChange={(val) => {
                              // 更新当前项的类别
                              const currentCategories = JSON.parse(inputs.audit_categories || '[]');
                              currentCategories[index] = `${val}:${threshold || '0.9'}`;
                              handleInputChange("audit_categories", JSON.stringify(currentCategories));
                            }}
                            optionList={availableOptions}
                          />
                          <Input
                            style={{width: 100}}
                            placeholder="阈值"
                            value={threshold}
                            onChange={(val) => {
                              // 更新当前项的阈值，仅接受数字和小数点
                              const numericValue = val.replace(/[^0-9.]/g, '');
                              const currentCategories = JSON.parse(inputs.audit_categories || '[]');
                              currentCategories[index] = `${category}:${numericValue}`;
                              handleInputChange("audit_categories", JSON.stringify(currentCategories));
                            }}
                          />
                          <Button
                            theme="borderless"
                            type="danger"
                            onClick={() => {
                              // 删除此项
                              const currentCategories = JSON.parse(inputs.audit_categories || '[]');
                              currentCategories.splice(index, 1);
                              handleInputChange("audit_categories", JSON.stringify(currentCategories));
                            }}
                          >
                            删除
                          </Button>
                        </div>
                      );
                    }) : []
                  }

                  {/* 添加新审核类别按钮 */}
                  <Button
                    style={{marginTop: 10}}
                    type="primary"
                    theme="light"
                    onClick={() => {
                      const currentCategories = JSON.parse(inputs.audit_categories || '[]');

                      // 获取所有已选类别
                      const selectedCategories = currentCategories.map(item => item.split(':')[0]);

                      // 查找第一个未被选择的类别
                      const allCategories = [
                        "harassment", "harassment/threatening", "hate", "hate/threatening",
                        "illicit", "illicit/violent", "self-harm", "self-harm/instructions",
                        "self-harm/intent", "sexual", "sexual/minors", "violence", "violence/graphic"
                      ];

                      const availableCategory = allCategories.find(cat => !selectedCategories.includes(cat));

                      if (availableCategory) {
                        currentCategories.push(`${availableCategory}:0.9`); // 添加一个默认项
                        handleInputChange("audit_categories", JSON.stringify(currentCategories));
                      } else {
                        // 所有类别都已选择，显示提示
                        showInfo('所有审核类别已添加');
                      }
                    }}
                  >
                    添加审核类别
                  </Button>
                </div>
              </div>


              {/* 审查API Key */}
              <div style={{marginBottom: 10}}>
                <Typography.Text strong>
                  审查密钥（API Key）<span style={{color: "#dc2626"}}>*</span>
                </Typography.Text>
                <Input
                  required={true}
                  name="audit_apikey"
                  placeholder="请输入OpenAI内容审核API密钥"
                  onChange={(v) => handleInputChange("audit_apikey", v)}
                  value={inputs.audit_apikey}
                  autoComplete="new-password"
                />
                {(!inputs.audit_apikey || inputs.audit_apikey.trim() === "") && (
                  <Typography.Text type="danger" style={{fontSize: 12}}>
                    审查开启后密钥为必填！
                  </Typography.Text>
                )}
              </div>
              {/* 审查接口地址 */}
              <div style={{marginBottom: 10}}>
                <Typography.Text strong>审查接口地址（选填）</Typography.Text>
                <Input
                  name="audit_url"
                  placeholder="默认：https://api.openai.com/v1/moderations"
                  onChange={(v) => handleInputChange("audit_url", v)}
                  value={inputs.audit_url}
                  autoComplete="new-password"
                />
                {!inputs.audit_url && (
                  <Typography.Text type="secondary" style={{fontSize: 12}}>
                    不填时自动使用 https://api.openai.com/v1/moderations
                  </Typography.Text>
                )}
              </div>
              {/* 审查模型 */}
              <div>
                <Typography.Text strong>审查模型（选填）</Typography.Text>
                <Input
                  name="audit_model"
                  placeholder="默认：omni-moderation-latest"
                  onChange={(v) => handleInputChange("audit_model", v)}
                  value={inputs.audit_model}
                  autoComplete="new-password"
                />
                {!inputs.audit_model && (
                  <Typography.Text type="secondary" style={{fontSize: 12}}>
                    不填时自动使用 omni-moderation-latest
                  </Typography.Text>
                )}
              </div>
            </div>
          )}


          <div style={{marginTop: 20, marginBottom: 20, border: '1px solid #eee', borderRadius: 6, padding: 12}}>
            <Typography.Text strong>
              计费补充规则
              <Typography.Text type="secondary" style={{fontSize: 12, marginLeft: 6}}>
                （可添加多项，用于分段定价，输入token阈值与倍率）
              </Typography.Text>
            </Typography.Text>
            {/* 列表渲染 */}
            {billingSupplementList.map((item, idx) => (
              <div key={idx} style={{display: 'flex', alignItems: 'center', gap: 8, marginTop: 8}}>
                <Input
                  style={{width: 240}}
                  placeholder="Token数阈值（如30000）"
                  value={item.tokenCount}
                  onChange={v => {
                    const list = [...billingSupplementList];
                    list[idx].tokenCount = v.replace(/\D/g, ''); // 只允许数字
                    setBillingSupplementList(list);
                  }}
                />
                <Input
                  style={{width: 180}}
                  placeholder="倍率（如2）"
                  value={item.multiplied}
                  onChange={v => {
                    const list = [...billingSupplementList];
                    list[idx].multiplied = v.replace(/\D/g, '');
                    setBillingSupplementList(list);
                  }}
                />
                <Button
                  theme="borderless"
                  type="danger"
                  onClick={() => {
                    // 删除
                    setBillingSupplementList(billingSupplementList.filter((_, i) => i !== idx));
                  }}
                >
                  删除
                </Button>
              </div>
            ))}
            <Button
              style={{marginTop: 10}}
              type="primary"
              theme="light"
              onClick={() => setBillingSupplementList([...billingSupplementList, {tokenCount: '', multiplied: ''}])}
            >新增规则</Button>
          </div>


          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('渠道优先级')}</Typography.Text>
          </div>
          <Input
            label={t('渠道优先级')}
            name='priority'
            placeholder={t('渠道优先级')}
            onChange={(value) => {
              const number = parseInt(value);
              if (isNaN(number)) {
                handleInputChange('priority', value);
              } else {
                handleInputChange('priority', number);
              }
            }}
            value={inputs.priority}
            autoComplete='new-password'
          />
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('渠道权重')}</Typography.Text>
          </div>
          <Input
            label={t('渠道权重')}
            name='weight'
            placeholder={t('渠道权重')}
            onChange={(value) => {
              const number = parseInt(value);
              if (isNaN(number)) {
                handleInputChange('weight', value);
              } else {
                handleInputChange('weight', number);
              }
            }}
            value={inputs.weight}
            autoComplete='new-password'
          />
          <>
            <div style={{marginTop: 10}}>
              <Typography.Text strong>{t('渠道额外设置')}：</Typography.Text>
            </div>
            <TextArea
              placeholder={
                t(
                  '此项可选，用于配置渠道特定设置，为一个 JSON 字符串，例如：',
                ) + '\n{\n  "force_format": true\n}'
              }
              name='setting'
              onChange={(value) => {
                handleInputChange('setting', value);
              }}
              autosize
              value={inputs.setting}
              autoComplete='new-password'
            />
            <Space>
              <Typography.Text
                style={{
                  color: 'rgba(var(--semi-blue-5), 1)',
                  userSelect: 'none',
                  cursor: 'pointer',
                }}
                onClick={() => {
                  handleInputChange(
                    'setting',
                    JSON.stringify(
                      {
                        force_format: true,
                      },
                      null,
                      2,
                    ),
                  );
                }}
              >
                {t('填入模板')}
              </Typography.Text>
              <Typography.Text
                style={{
                  color: 'rgba(var(--semi-blue-5), 1)',
                  userSelect: 'none',
                  cursor: 'pointer',
                }}
                onClick={() => {
                  window.open(
                    'https://github.com/Calcium-Ion/new-api/blob/main/docs/channel/other_setting.md',
                  );
                }}
              >
                {t('设置说明')}
              </Typography.Text>
            </Space>
          </>
          <>
            <div style={{marginTop: 10}}>
              <Typography.Text strong>{t('参数覆盖')}：</Typography.Text>
            </div>
            <TextArea
              placeholder={
                t(
                  '此项可选，用于覆盖请求参数。不支持覆盖 stream 参数。为一个 JSON 字符串，例如：',
                ) + '\n{\n  "temperature": 0\n}'
              }
              name='setting'
              onChange={(value) => {
                handleInputChange('param_override', value);
              }}
              autosize
              value={inputs.param_override}
              autoComplete='new-password'
            />
          </>
          {inputs.type === 1 && (
            <>
              <div style={{marginTop: 10}}>
                <Typography.Text strong>{t('组织')}：</Typography.Text>
              </div>
              <Input
                label={t('组织，可选，不填则为默认组织')}
                name='openai_organization'
                placeholder={t('请输入组织org-xxx')}
                onChange={(value) => {
                  handleInputChange('openai_organization', value);
                }}
                value={inputs.openai_organization}
              />
            </>
          )}
          <div style={{marginTop: 10}}>
            <Typography.Text strong>{t('默认测试模型')}：</Typography.Text>
          </div>
          <Input
            name='test_model'
            placeholder={t('不填则为模型列表第一个')}
            onChange={(value) => {
              handleInputChange('test_model', value);
            }}
            value={inputs.test_model}
          />
          <div style={{marginTop: 10, display: 'flex'}}>
            <Space>
              <Checkbox
                name='auto_ban'
                checked={autoBan}
                onChange={() => {
                  setAutoBan(!autoBan);
                }}
              />
              <Typography.Text strong>
                {t(
                  '是否自动禁用（仅当自动禁用开启时有效），关闭后不会自动禁用该渠道：',
                )}
              </Typography.Text>
            </Space>
          </div>
          <div style={{marginTop: 10}}>
            <Typography.Text strong>
              {t('状态码复写（仅影响本地判断，不修改返回到上游的状态码）')}：
            </Typography.Text>
          </div>
          <TextArea
            placeholder={
              t(
                '此项可选，用于复写返回的状态码，比如将claude渠道的400错误复写为500（用于重试），请勿滥用该功能，例如：',
              ) +
              '\n' +
              JSON.stringify(STATUS_CODE_MAPPING_EXAMPLE, null, 2)
            }
            name='status_code_mapping'
            onChange={(value) => {
              handleInputChange('status_code_mapping', value);
            }}
            autosize
            value={inputs.status_code_mapping}
            autoComplete='new-password'
          />
          <Typography.Text
            style={{
              color: 'rgba(var(--semi-blue-5), 1)',
              userSelect: 'none',
              cursor: 'pointer',
            }}
            onClick={() => {
              handleInputChange(
                'status_code_mapping',
                JSON.stringify(STATUS_CODE_MAPPING_EXAMPLE, null, 2),
              );
            }}
          >
            {t('填入模板')}
          </Typography.Text>
        </Spin>
        <ImagePreview
          src={modalImageUrl}
          visible={isModalOpenurl}
          onVisibleChange={(visible) => setIsModalOpenurl(visible)}
        />
      </SideSheet>
    </>
  );
};

export default EditChannel;
