<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api.ts';
import { targets } from '../config.ts';
import type { Probe, Target } from '../api.ts';
import { yamlPreviewText } from '../yaml-preview.ts';

const mode = ref<'blank' | 'file' | 'url'>('blank');
const name = ref('');
const target = ref<Target>('both');
const file = ref<File>();
const url = ref('');
const userAgent = ref('');
const result = ref<Probe | null>(null);
const previewYAML = computed(() => yamlPreviewText(result.value?.yaml ?? ''));
const busy = ref(false);
const error = ref('');

watch([mode, file, url, userAgent], () => { result.value = null; error.value = ''; });

async function preview() {
  busy.value = true;
  error.value = '';
  const sourceMode = mode.value;
  const sourceFile = file.value;
  const sourceURL = url.value.trim();
  const agent = userAgent.value.trim();
  try {
    if (sourceMode === 'file' && !sourceFile) throw new Error('请选择 YAML 文件。');
    if (sourceMode === 'file' && sourceFile && sourceFile.size > 1024 * 1024) throw new Error('YAML 文件不能超过 1 MB。');
    if (sourceMode === 'url' && !sourceURL) throw new Error('请填写原始订阅地址。');
    const probe = await api.probe({
      mode: 'config',
      ...(sourceMode === 'url' ? { url: sourceURL, userAgent: agent || undefined } : { yaml: await sourceFile?.text() }),
    });
    if (mode.value !== sourceMode || file.value !== sourceFile || url.value.trim() !== sourceURL || userAgent.value.trim() !== agent) return;
    if (probe.kind !== 'config' || !probe.id) throw new Error('该来源可以在配置的「订阅源」模块中添加。');
    result.value = probe;
    if (!name.value && sourceFile) name.value = sourceFile.name.replace(/\.ya?ml$/i, '').slice(0, 48);
  } catch (e) {
    error.value = e instanceof Error ? e.message : '预览导入失败。';
  } finally {
    busy.value = false;
  }
}

function data() {
  if (!name.value.trim()) throw new Error('请填写配置名称。');
  if (mode.value !== 'blank' && !result.value?.id) throw new Error('请先预览导入内容。');
  return {
    name: name.value.trim(), target: target.value,
    ...(mode.value !== 'blank' ? { importId: result.value?.id } : {}),
  };
}

defineExpose({ data });
</script>

<template>
  <div class="form-grid">
    <label class="field">配置名称<input v-model="name" required maxlength="48" /></label>
    <label class="field">目标客户端
      <select v-model="target">
        <option v-for="(label, key) in targets" :key="key" :value="key">{{ label }}</option>
      </select>
    </label>
    <div class="form-switch full" aria-label="配置来源">
      <button class="btn" :class="mode === 'blank' ? 'selected' : 'bare'" type="button" :aria-pressed="mode === 'blank'"
        :disabled="busy" @click="mode = 'blank'">空白配置</button>
      <button class="btn" :class="mode === 'file' ? 'selected' : 'bare'" type="button" :aria-pressed="mode === 'file'"
        :disabled="busy" @click="mode = 'file'">导入 YAML</button>
      <button class="btn" :class="mode === 'url' ? 'selected' : 'bare'" type="button" :aria-pressed="mode === 'url'"
        :disabled="busy" @click="mode = 'url'">原始订阅地址</button>
    </div>
    <label v-if="mode === 'file'" class="field full">YAML 文件
      <input type="file" accept=".yaml,.yml,application/yaml,text/yaml" :disabled="busy"
        @change="file = ($event.target as HTMLInputElement).files?.[0]" />
    </label>
    <template v-if="mode === 'url'">
      <label class="field full">完整配置订阅地址
        <input v-model="url" type="url" placeholder="https://" :disabled="busy" autocomplete="off" />
      </label>
      <label class="field full">请求 User-Agent（可选）<input v-model="userAgent" :disabled="busy" autocomplete="off" /></label>
    </template>
    <div v-if="mode !== 'blank'" class="full import-preview">
      <button class="btn secondary" type="button" :disabled="busy" @click="preview">{{ busy ? '正在读取…' : '预览导入' }}</button>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div v-if="result" class="import-result">
        <p>已读取 YAML{{ result.host ? ' · ' + result.host : '' }}</p>
        <p v-if="result.issues?.length" class="muted">发现 {{ result.issues.length }} 条校验结果，可创建草稿后查看和修复。</p>
        <pre v-if="previewYAML" tabindex="0" aria-label="导入 YAML 预览">{{ previewYAML }}</pre>
      </div>
    </div>
  </div>
</template>
