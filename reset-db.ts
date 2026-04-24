import pg from 'pg';

const { Pool } = pg;

async function resetDatabase() {
  const pool = new Pool({
    connectionString: process.env.DATABASE_URL || 'postgres://user:password@localhost:5432/rental?sslmode=disable',
  });

  try {
    console.log('🔄 Limpando dados do banco...');
    
    await pool.query('TRUNCATE TABLE receipts, leases, properties, tenants, users RESTART IDENTITY CASCADE');
    
    console.log('✅ Dados removidos com sucesso!');
    
    const result = await pool.query('SELECT COUNT(*) as count FROM users');
    console.log(`📊 Usuários restantes: ${result.rows[0].count}`);
    
  } catch (error) {
    console.error('❌ Erro ao limpar banco:', error);
    process.exit(1);
  } finally {
    await pool.end();
  }
}

resetDatabase();
