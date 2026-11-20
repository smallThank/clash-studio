<script setup lang="ts">
import { onMounted, useTemplateRef, watch } from 'vue';
import Icon from './Icon.vue';

const props = defineProps<{ title: string; submit?: string; busy?: boolean; error?: string }>();
const emit = defineEmits<{ close: []; submit: [] }>();
const dialog = useTemplateRef<HTMLDialogElement>('dialog');
const feedback = useTemplateRef<HTMLParagraphElement>('feedback');

onMounted(() => {
  dialog.value?.showModal();
  dialog.value?.querySelector<HTMLElement>('.dialog-body input, .dialog-body select, .dialog-body textarea')?.focus();
});
watch(() => props.error, error => { if (error) feedback.value?.focus(); }, { flush: 'post' });
</script>

<template>
  <dialog ref="dialog" aria-labelledby="dialog-title" @cancel.prevent="!busy && emit('close')">
    <div class="dialog-head">
      <h2 id="dialog-title">{{ title }}</h2>
      <button class="icon-btn" type="button" aria-label="关闭" :disabled="busy" @click="emit('close')"><Icon name="close" :size="16" /></button>
    </div>
    <form id="dialog-form" @submit.prevent="emit('submit')">
      <div class="dialog-body">
        <slot />
        <p v-if="error" ref="feedback" class="form-error" role="alert" tabindex="-1">{{ error }}</p>
      </div>
      <div class="dialog-actions">
        <slot name="actions" />
        <div class="dialog-spacer" />
        <button type="button" class="btn secondary" :disabled="busy" @click="emit('close')">{{ submit ? '取消' : '关闭' }}</button>
        <button v-if="submit" type="submit" class="btn primary" :disabled="busy">{{ submit }}</button>
      </div>
    </form>
  </dialog>
</template>
