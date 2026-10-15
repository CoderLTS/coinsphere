<template>
  <main class="user-center-page">
    <header class="workspace-heading"><h1>个人账号</h1><p>查看账号信息，管理登录密码。</p></header>
    <div class="account-workspace">
      <ElCard shadow="never" class="account-summary">
        <div class="account-identity"
          ><img :src="userAvatar" alt="用户头像" /><div
            ><h2>{{ userInfo.username || '未登录用户' }}</h2
            ><span>个人账号</span></div
          ></div
        >
        <ElDescriptions :column="1" size="small">
          <ElDescriptionsItem label="用户名">{{ userInfo.username || '—' }}</ElDescriptionsItem>
          <ElDescriptionsItem v-if="userInfo.email" label="邮箱">{{
            userInfo.email
          }}</ElDescriptionsItem>
        </ElDescriptions>
        <p>用于个人工作流、插件配置和系统运维。</p>
      </ElCard>
      <ElCard shadow="never" class="account-security">
        <template #header><h2>修改密码</h2></template>
        <ElAlert v-if="error" :title="error" type="error" :closable="false" />
        <ElForm
          ref="passwordForm"
          :model="passwords"
          :rules="passwordRules"
          :disabled="saving"
          size="small"
          label-position="top"
          @submit.prevent="changePassword"
        >
          <ElFormItem label="当前密码" prop="currentPassword"
            ><ElInput
              v-model="passwords.currentPassword"
              type="password"
              autocomplete="current-password"
              show-password
          /></ElFormItem>
          <ElFormItem label="新密码" prop="newPassword"
            ><ElInput
              v-model="passwords.newPassword"
              type="password"
              autocomplete="new-password"
              show-password
          /></ElFormItem>
          <ElFormItem label="确认新密码" prop="confirmation"
            ><ElInput
              v-model="passwords.confirmation"
              type="password"
              autocomplete="new-password"
              show-password
          /></ElFormItem>
          <p>修改后所有登录会话失效，请使用新密码重新登录。</p>
          <ElButton type="primary" native-type="submit" :loading="saving">修改密码</ElButton>
        </ElForm>
      </ElCard>
    </div>
  </main>
</template>
<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { fetchChangePassword } from '@/api/auth'
  import defaultAvatar from '@imgs/user/avatar.webp'
  import { useUserStore } from '@/store/modules/user'

  defineOptions({ name: 'UserCenter' })

  const userStore = useUserStore()
  const userInfo = computed(() => userStore.getUserInfo)
  const userAvatar = computed(() => userInfo.value.avatar || defaultAvatar)

  const passwordForm = ref<FormInstance>()
  const passwords = reactive({ currentPassword: '', newPassword: '', confirmation: '' })
  const saving = ref(false)
  const error = ref('')
  const passwordRules: FormRules = {
    currentPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
    newPassword: [
      { required: true, message: '请输入新密码', trigger: 'blur' },
      { min: 6, message: '新密码至少 6 个字符', trigger: 'blur' }
    ],
    confirmation: [
      {
        validator: (_rule, _value, callback) =>
          callback(
            passwords.confirmation === passwords.newPassword
              ? undefined
              : new Error('两次新密码不一致')
          ),
        trigger: 'blur'
      }
    ]
  }
  const changePassword = async () => {
    if (!passwordForm.value || saving.value) return
    if (!(await passwordForm.value.validate().catch(() => false))) return
    saving.value = true
    error.value = ''
    try {
      await fetchChangePassword(passwords.currentPassword, passwords.newPassword)
      ElMessage.success('密码已修改，请重新登录')
      userStore.clearSession()
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      saving.value = false
      passwords.currentPassword = ''
      passwords.newPassword = ''
      passwords.confirmation = ''
    }
  }
</script>
<style scoped>
  .user-center-page {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .account-workspace {
    display: grid;
    grid-template-columns: minmax(220px, 300px) minmax(0, 600px);
    gap: 16px;
    align-items: start;
  }
  .account-identity {
    display: flex;
    gap: 12px;
    align-items: center;
    margin-bottom: 20px;
  }
  .account-identity img {
    width: 48px;
    height: 48px;
    border-radius: 8px;
    object-fit: cover;
  }
  h2 {
    margin: 0 0 6px;
    font-size: 16px;
  }
  p,
  .account-identity span {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    line-height: 1.6;
  }
  .el-alert {
    margin-bottom: 16px;
  }
  @media (max-width: 768px) {
    .account-workspace {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
