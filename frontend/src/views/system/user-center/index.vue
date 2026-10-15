<!-- 页面或页面组件：index。 -->
<template>
  <div class="user-center-page">
    <ElRow :gutter="20">
      <ElCol :xs="24" :lg="8">
        <ElCard shadow="never" class="profile-card">
          <div class="profile-card__banner"></div>
          <img class="profile-card__avatar" :src="userAvatar" alt="用户头像" />
          <div class="profile-card__name">{{ userInfo.username || '未登录用户' }}</div>
          <div class="profile-card__role">个人账号</div>
          <div class="profile-card__desc">个人工作流与系统运维</div>

          <div class="profile-card__meta">
            <div class="meta-item">
              <ArtSvgIcon icon="ri:mail-line" />
              <span>{{ userInfo.email || '--' }}</span>
            </div>
            <div class="meta-item">
              <ArtSvgIcon icon="ri:shield-user-line" />
              <span>个人账号</span>
            </div>
            <div class="meta-item">
              <ArtSvgIcon icon="ri:user-smile-line" />
              <span>coinsphere</span>
            </div>
          </div>
        </ElCard>
      </ElCol>

      <ElCol :xs="24" :lg="16">
        <ElCard shadow="never" class="detail-card">
          <template #header>
            <div class="detail-card__header">
              <span>账户信息</span>
            </div>
          </template>
          <ElDescriptions :column="1" border>
            <ElDescriptionsItem label="用户名">{{ userInfo.username || '--' }}</ElDescriptionsItem>
            <ElDescriptionsItem label="邮箱">{{ userInfo.email || '--' }}</ElDescriptionsItem>
          </ElDescriptions>
        </ElCard>

        <ElCard shadow="never" class="detail-card">
          <template #header>
            <div class="detail-card__header">
              <span>修改密码</span>
            </div>
          </template>
          <ElForm
            ref="passwordForm"
            :model="passwords"
            :rules="passwordRules"
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
      </ElCol>
    </ElRow>
  </div>
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
    if (!passwordForm.value) return
    await passwordForm.value.validate()
    saving.value = true
    try {
      await fetchChangePassword(passwords.currentPassword, passwords.newPassword)
      ElMessage.success('密码已修改，请重新登录')
      userStore.clearSession()
    } finally {
      saving.value = false
      passwords.currentPassword = ''
      passwords.newPassword = ''
      passwords.confirmation = ''
    }
  }
</script>

<style scoped lang="scss">
  .user-center-page {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .profile-card,
  .detail-card {
    border-radius: 20px;
  }

  .profile-card {
    position: relative;
    overflow: hidden;
    text-align: center;

    :deep(.el-card__body) {
      padding: 0 24px 24px;
    }
  }

  .profile-card__banner {
    height: 128px;
    margin: 0 -24px;
    background:
      linear-gradient(140deg, rgb(93 135 255 / 0.85), rgb(56 192 252 / 0.45)),
      linear-gradient(180deg, #f8fbff, #eef5ff);
  }

  .profile-card__avatar {
    width: 84px;
    height: 84px;
    margin-top: -42px;
    object-fit: cover;
    border: 4px solid #fff;
    border-radius: 50%;
  }

  .profile-card__name {
    margin-top: 14px;
    font-size: 24px;
    font-weight: 700;
    color: var(--art-gray-900);
  }

  .profile-card__role {
    margin-top: 8px;
    font-size: 13px;
    color: var(--art-gray-500);
  }

  .profile-card__desc {
    margin-top: 14px;
    font-size: 14px;
    line-height: 1.8;
    color: var(--art-gray-600);
  }

  .profile-card__meta {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-top: 24px;
    text-align: left;
  }

  .meta-item {
    display: flex;
    gap: 10px;
    align-items: center;
    font-size: 14px;
    color: var(--art-gray-600);
  }

  .detail-card {
    margin-bottom: 20px;

    :deep(.el-card__header) {
      padding: 18px 22px 14px;
    }

    :deep(.el-card__body) {
      padding: 0 22px 22px;
    }
  }

  .detail-card__header {
    font-size: 16px;
    font-weight: 600;
    color: var(--art-gray-900);
  }

  .explain-block {
    padding-top: 18px;
    font-size: 14px;
    line-height: 1.9;
    color: var(--art-gray-600);

    p {
      margin: 0 0 12px;
    }
  }
</style>
